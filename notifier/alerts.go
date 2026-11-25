package notifier

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util"
)

// TemplateFiles holds the notification templates. main.go assigns the embedded
// templates here; it is an fs.FS interface (which embed.FS satisfies) so tests
// can supply an on-disk template directory instead.
var TemplateFiles fs.FS

// NotifCacheSet and NotifCacheGet are set by the events package to avoid circular imports.
// They provide access to the notification message ID cache.
var NotifCacheSet func(reviewID, provider, messageID string)
var NotifCacheGet func(reviewID, provider string) string

// ReviewNotifiedSet is set by the events package to mark a review as notified,
// recording the event its alert led with.
var ReviewNotifiedSet func(reviewID, eventID string)

// alertDeliveries holds the provider sends still running for a review's latest
// alert, so a GenAI update arriving meanwhile waits for them and edits the
// delivered message instead of sending a second alert.
var alertDeliveries sync.Map // review ID -> *sync.WaitGroup

type notifMeta struct {
	name  string
	index int
}

// sendOptions controls one sendAlert dispatch.
//
//   - bypassCooldown skips the cooldown reservation (manual tests and GenAI
//     escalations, which must not be dropped by an unrelated alert's cooldown).
//   - isTest marks a manual test: it also skips the mute gate and the hourly
//     cap, and the history entry is tagged "test".
//   - match narrows the dispatch to selected senders (targeted tests and
//     escalations); routing rules are only applied when match is nil.
//   - isEscalation records the dispatch as a GenAI escalation, not a plain
//     alert; skipEscalation stops an escalation dispatch from re-evaluating
//     the escalation rules.
type sendOptions struct {
	bypassCooldown bool
	isTest         bool
	match          func(alertSender) bool
	isEscalation   bool
	skipEscalation bool
}

// SendAlert forwards alert information to all enabled alerting methods.
func SendAlert(events []models.Event) {
	sendAlert(events, sendOptions{})
}

// SendTestAlert sends a manual test through provider filters without consuming a cooldown.
func SendTestAlert(events []models.Event) {
	sendAlert(events, sendOptions{bypassCooldown: true, isTest: true})
}

// SendTestAlertTo sends a manual test to a single provider profile.
func SendTestAlertTo(events []models.Event, name string, index int) {
	sendAlert(events, sendOptions{bypassCooldown: true, isTest: true, match: func(s alertSender) bool { return s.name == name && s.index == index }})
}

// SendTestAlertToProvider sends a manual test to every profile of one provider.
func SendTestAlertToProvider(events []models.Event, name string) {
	sendAlert(events, sendOptions{bypassCooldown: true, isTest: true, match: func(s alertSender) bool { return s.name == name }})
}

func sendAlert(events []models.Event, opts sendOptions) {
	if len(events) == 0 {
		return
	}
	isGenAIUpdate := events[0].Extra.IsGenAIUpdate
	var deliveries sync.WaitGroup
	if events[0].Extra.ReviewID != "" {
		reviewID := events[0].Extra.ReviewID
		if isGenAIUpdate {
			awaitAlertDelivery(reviewID)
		} else {
			// Hold the delivery open until every provider send has been started
			deliveries.Add(1)
			alertDeliveries.Store(reviewID, &deliveries)
			// Mark the review notified only once its delivery can be waited on
			if ReviewNotifiedSet != nil {
				ReviewNotifiedSet(reviewID, events[0].ID)
			}
			defer func() {
				deliveries.Done()
				go func() {
					deliveries.Wait()
					alertDeliveries.CompareAndDelete(reviewID, &deliveries)
				}()
			}()
		}
	}

	// Reset summary idle timer (skip for GenAI updates to avoid re-triggering)
	if !isGenAIUpdate {
		ResetSummaryTimer()
	}

	// Collect snapshot, if available.
	var snapshot io.Reader
	for _, event := range events {
		if event.HasSnapshot {
			snapshot = GetSnapshot(event)
			break
		}
	}

	// Only alerts with at least one eligible destination can consume a cooldown.
	senders := eligibleAlertSenders(events)
	if opts.match != nil {
		var kept []alertSender
		for _, s := range senders {
			if opts.match(s) {
				kept = append(kept, s)
			}
		}
		senders = kept
	} else {
		// Routing rules only narrow real alerts; targeted test alerts
		// bypass routing.
		senders = applyRouting(senders, events)
	}
	if len(senders) == 0 {
		return
	}

	// Per-camera mute: drop the alert without consuming a cooldown or
	// stamping LastNotification, and record the drop in history. Test
	// alerts bypass the mute.
	if !opts.isTest {
		if muted, until := IsMuted(events[0].Camera); muted {
			log.Info().Str("camera", events[0].Camera).Time("muted_until", until).Msg("Notification dropped - Camera muted")
			RecordMutedDrop(events[0], until)
			return
		}
	}

	// Reserve once for the entire alert, including all detections in a review.
	// Event, review, and audio-only notifications all pass through this gate.
	// GenAI updates never consume a cooldown: they edit the message the
	// initial alert already sent, so a cooldown must not drop the caption edit.
	if !opts.bypassCooldown && !isGenAIUpdate && !notificationCooldown.allow(events[0].Camera, config.ConfigData.Alerts.General) {
		log.Info().Str("camera", events[0].Camera).Msg("Notification dropped - Cooldown active")
		return
	}

	// Hourly cap: a rolling 1-hour window over DISPATCHED alerts, global
	// across providers. Only alerts that reach this gate count — filtered,
	// muted and cooled-down alerts never arrive here. Test alerts are
	// exempt; GenAI updates and escalations count, because the user sees
	// them as alerts.
	if !opts.isTest && !hourlyCap.allow(config.ConfigData.Alerts.General.MaxNotificationsPerHour) {
		log.Info().Str("camera", events[0].Camera).Msg("Notification dropped - Hourly cap reached")
		if opts.isEscalation {
			RecordEscalationHistory(events[0], nil)
		} else {
			RecordCapDrop(events[0])
		}
		return
	}
	config.Internal.Status.LastNotification = time.Now()

	// Record the dispatch for the Web UI history (in-memory only).
	if opts.isEscalation {
		RecordEscalationHistory(events[0], senderNames(senders))
	} else {
		RecordHistory(events[0], senderNames(senders), isGenAIUpdate, opts.isTest)
	}

	// Set extra event details & get event used for notifications
	event := setExtras(events)

	// Create copy of snapshot for each alerting method
	var snap []byte
	if snapshot != nil {
		snap, _ = io.ReadAll(snapshot)
	} else {
		event.HasSnapshot = false
	}

	for _, sender := range senders {
		deliveries.Go(func() { sender.send(event, snap) })
	}

	// A GenAI update can escalate: when its threat level reaches an
	// escalation rule, a FRESH alert goes to that rule's targets in addition
	// to the caption edit dispatched above. The escalation is a separate
	// sendAlert call (own cooldown bypass, own history entry), and it runs
	// after the edit so the two never race for the same message.
	if isGenAIUpdate && !opts.skipEscalation {
		escalateIfNeeded(events)
	}
}

// senderNames returns the deduplicated provider names of eligible senders,
// in first-seen order, for the history record.
func senderNames(senders []alertSender) []string {
	var names []string
	seen := make(map[string]bool)
	for _, s := range senders {
		if !seen[s.name] {
			seen[s.name] = true
			names = append(names, s.name)
		}
	}
	return names
}

// alertSender is one eligible provider destination, kept with its name and
// profile index so the Web UI history can list where an alert went and a
// targeted test can select exactly one profile. filters is the profile's
// AlertFilter, carried so eligibleAlertSenders can apply it without
// re-reading the config.
type alertSender struct {
	name    string
	index   int
	filters models.AlertFilter
	send    func(models.Event, []byte)
}

// allAlertSenders lists every enabled provider profile, without applying any
// filters. Event summaries reuse this list directly: a summary covers all
// cameras, so per-provider filters must not drop it.
func allAlertSenders() []alertSender {
	var senders []alertSender
	// Send Alerts
	// Apprise API
	for id, profile := range config.ConfigData.Alerts.AppriseAPI {
		if profile.Enabled {
			provider := notifMeta{name: "apprise_api", index: id}
			senders = append(senders, alertSender{name: "apprise_api", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendAppriseAPI(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Bark
	for id, profile := range config.ConfigData.Alerts.Bark {
		if profile.Enabled {
			provider := notifMeta{name: "bark", index: id}
			senders = append(senders, alertSender{name: "bark", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendBark(event, provider)
			}})
		}
	}
	// Discord
	for id, profile := range config.ConfigData.Alerts.Discord {
		if profile.Enabled {
			provider := notifMeta{name: "discord", index: id}
			senders = append(senders, alertSender{name: "discord", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendDiscordMessage(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Gotify
	for id, profile := range config.ConfigData.Alerts.Gotify {
		if profile.Enabled {
			provider := notifMeta{name: "gotify", index: id}
			senders = append(senders, alertSender{name: "gotify", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendGotifyPush(event, provider)
			}})
		}
	}
	// Matrix
	for id, profile := range config.ConfigData.Alerts.Matrix {
		if profile.Enabled {
			provider := notifMeta{name: "matrix", index: id}
			senders = append(senders, alertSender{name: "matrix", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendMatrix(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Mattermost
	for id, profile := range config.ConfigData.Alerts.Mattermost {
		if profile.Enabled {
			provider := notifMeta{name: "mattermost", index: id}
			senders = append(senders, alertSender{name: "mattermost", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendMattermost(event, provider)
			}})
		}
	}
	// Ntfy
	for id, profile := range config.ConfigData.Alerts.Ntfy {
		if profile.Enabled {
			provider := notifMeta{name: "ntfy", index: id}
			senders = append(senders, alertSender{name: "ntfy", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendNtfyPush(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Pushover
	for id, profile := range config.ConfigData.Alerts.Pushover {
		if profile.Enabled {
			provider := notifMeta{name: "pushover", index: id}
			senders = append(senders, alertSender{name: "pushover", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendPushoverMessage(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Signal
	for id, profile := range config.ConfigData.Alerts.Signal {
		if profile.Enabled {
			provider := notifMeta{name: "signal", index: id}
			senders = append(senders, alertSender{name: "signal", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendSignalMessage(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// SMTP
	for id, profile := range config.ConfigData.Alerts.SMTP {
		if profile.Enabled {
			provider := notifMeta{name: "smtp", index: id}
			senders = append(senders, alertSender{name: "smtp", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendSMTP(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Telegram
	for id, profile := range config.ConfigData.Alerts.Telegram {
		if profile.Enabled {
			provider := notifMeta{name: "telegram", index: id}
			senders = append(senders, alertSender{name: "telegram", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendTelegramMessage(event, bytes.NewReader(snap), provider)
			}})
		}
	}
	// Webhook
	for id, profile := range config.ConfigData.Alerts.Webhook {
		if profile.Enabled {
			provider := notifMeta{name: "webhook", index: id}
			senders = append(senders, alertSender{name: "webhook", index: id, filters: profile.Filters, send: func(event models.Event, snap []byte) {
				SendWebhook(event, provider)
			}})
		}
	}
	return senders
}

// eligibleAlertSenders applies each profile's filters before reserving a cooldown.
func eligibleAlertSenders(events []models.Event) []alertSender {
	var senders []alertSender
	for _, sender := range allAlertSenders() {
		if checkAlertFilters(events, sender.filters, notifMeta{name: sender.name, index: sender.index}) {
			senders = append(senders, sender)
		}
	}
	return senders
}

// awaitAlertDelivery waits until no alert for reviewID is being delivered,
// including alerts for the review that start while it waits
func awaitAlertDelivery(reviewID string) {
	var waited any
	for {
		pending, ok := alertDeliveries.Load(reviewID)
		if !ok || pending == waited {
			return
		}
		log.Debug().
			Str("review_id", reviewID).
			Msg("Waiting for alert delivery before sending GenAI update")
		pending.(*sync.WaitGroup).Wait()
		waited = pending
	}
}

func buildSnapshotURL(event models.Event) *url.URL {
	var snapurl *url.URL
	if config.ConfigData.Alerts.General.SnapHiRes {
		evtTime := fmt.Sprintf("%v", event.StartTime)
		snapurl, _ = url.Parse(config.ConfigData.Frigate.Server + "/api/" + event.Camera + "/recordings/" + evtTime + "/snapshot.jpg")
	} else {
		// Add optional snapshot modifiers
		snapurl, _ = url.Parse(config.ConfigData.Frigate.Server + "/api/events/" + event.ID + "/snapshot.jpg")
		q := snapurl.Query()
		if config.ConfigData.Alerts.General.SnapBbox {
			q.Add("bbox", "1")
		}
		if config.ConfigData.Alerts.General.SnapTimestamp {
			q.Add("timestamp", "1")
		}
		if config.ConfigData.Alerts.General.SnapCrop {
			q.Add("crop", "1")
		}
		snapurl.RawQuery = q.Encode()
	}
	return snapurl
}

// GetSnapshot downloads a snapshot from Frigate
func GetSnapshot(event models.Event) io.Reader {
	snapurl := buildSnapshotURL(event)

	var response []byte

	attempts := 0
	max_attempts := config.ConfigData.Alerts.General.MaxSnapRetry
	for attempts < max_attempts {
		var err error
		response, err = util.HTTPGet(snapurl.String(), config.ConfigData.Frigate.Insecure, "", config.ConfigData.Frigate.Headers...)
		if err != nil {
			attempts += 1
			if err.Error() == "404" {
				time.Sleep(2 * time.Second)
				log.Info().
					Str("event_id", event.ID).
					Int("attempt", attempts).
					Int("max_attempts", max_attempts).
					Msgf("Waiting for snapshot to be available")
				continue
			} else {
				log.Warn().
					Str("event_id", event.ID).
					Err(err).
					Msgf("Could not access snapshot")
				return nil
			}
		} else {
			break
		}
	}
	if attempts == max_attempts {
		return nil
	}
	return bytes.NewReader(response)
}

// GetClip downloads a event video clip from Frigate
func GetClip(event models.Event) io.Reader {
	clipurl := config.ConfigData.Frigate.Server + "/api/events/" + event.ID + "/clip.mp4"
	var response []byte

	attempts := 0
	max_attempts := config.ConfigData.Alerts.General.MaxSnapRetry
	for attempts < max_attempts {
		var err error
		response, err = util.HTTPGet(clipurl, config.ConfigData.Frigate.Insecure, "", config.ConfigData.Frigate.Headers...)
		if err != nil {
			attempts += 1
			if err.Error() == "404" {
				time.Sleep(2 * time.Second)
				log.Info().
					Str("event_id", event.ID).
					Int("attempt", attempts).
					Int("max_attempts", max_attempts).
					Msgf("Waiting for clip to be available")
				continue
			} else {
				log.Warn().
					Str("event_id", event.ID).
					Err(err).
					Msgf("Could not access clip")
				return nil
			}
		} else if len(response) == 0 {
			// Frigate is probably still processing the clip
			// Wait a bit and try again
			attempts += 1
			time.Sleep(2 * time.Second)
			log.Info().
				Str("event_id", event.ID).
				Int("attempt", attempts).
				Int("max_attempts", max_attempts).
				Msgf("Waiting for clip to be available")
		} else {
			log.Debug().
				Str("event_id", event.ID).
				Msgf("Clip available")
			break
		}
	}
	if attempts == max_attempts {
		return nil
	}
	return bytes.NewReader(response)
}

// setExtras adds additional data into the event model to be used for templates
func setExtras(events []models.Event) models.Event {
	// Pull first event, which will be used to store info relevant to notifications
	key := events[0]

	// Set Event link
	key.Extra.EventLink = config.ConfigData.Frigate.PublicURL + "/api/events/" + key.ID + "/clip.mp4"

	// Add Frigate Major version metadata
	key.Extra.FrigateMajorVersion = config.Internal.FrigateVersion

	// Transform camera names, example: "test_camera" to "Test Camera"
	caser := cases.Title(language.Und)
	key.Extra.CameraName = caser.String(strings.ReplaceAll(key.Camera, "_", " "))

	// Assign Frigate URL to extra event fields
	key.Extra.LocalURL = config.ConfigData.Frigate.Server
	key.Extra.PublicURL = config.ConfigData.Frigate.PublicURL

	// Create list of all detected objects / license plates (mostly applicable to /reviews)
	var labelList []string
	var sublabelList []string
	var licenseplateList []string
	for _, event := range events {
		if !slices.Contains(labelList, event.Label) {
			labelList = append(labelList, event.Label)
		}
		if !slices.Contains(sublabelList, event.SubLabel) {
			sublabelList = append(sublabelList, event.SubLabel)
		}
		if !slices.Contains(licenseplateList, event.Data.RecognizedLicensePlate) {
			licenseplateList = append(licenseplateList, event.Data.RecognizedLicensePlate)
		}
	}
	key.Extra.LabelList = strings.Join(labelList, ", ")
	key.Extra.SubLabelList = strings.Join(sublabelList, ", ")
	key.Extra.LicensePlateList = strings.Join(licenseplateList, ", ")

	// MQTT uses CurrentZones, Web API uses Zones
	// Combine into one object to use regardless of connection method
	for _, event := range events {
		key.Zones = append(key.Zones, event.CurrentZones...)
	}
	// Remove duplicates
	slices.Sort(key.Zones)
	key.Zones = slices.Compact(key.Zones)
	// Join zones into plain comma-separated string
	key.Extra.ZoneList = strings.Join(key.Zones, ", ")

	// If certain time format is provided, re-format date / time string
	eventTime := time.Unix(int64(key.StartTime), 0)
	key.Extra.UnixStartTime = eventTime.Unix()
	key.Extra.FormattedTime = eventTime.String()
	if config.ConfigData.Alerts.General.TimeFormat != "" {
		key.Extra.FormattedTime = eventTime.Format(config.ConfigData.Alerts.General.TimeFormat)
	}

	// Calc TopScore percentage
	key.Extra.TopScorePercent = fmt.Sprintf("%v%%", int((key.TopScore * 100)))

	// Calc License Plate score percentage
	key.Extra.LicensePlatePercent = fmt.Sprintf("%v%%", int((key.Data.RecognizedLicensePlateScore * 100)))

	// GenAI object description
	key.Extra.Description = key.Data.Description

	return key
}

// Build notification based on template
func renderMessage(sourceTemplate string, event models.Event, mtype string, provider string) string {
	// Render template
	var tmpl *template.Template
	var err error
	if sourceTemplate == "markdown" || sourceTemplate == "plaintext" || sourceTemplate == "html" || sourceTemplate == "json" {
		tmpl = template.Must(template.ParseFS(TemplateFiles, "templates/"+sourceTemplate+".template"))
	} else {
		tmpl, err = template.New("custom").Funcs(template.FuncMap{"env": includeenv}).Parse(sourceTemplate)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to render event message")
		}
	}

	var renderedTemplate bytes.Buffer
	err = tmpl.Execute(&renderedTemplate, event)
	if err != nil {
		log.Fatal().
			Err(err).
			Msgf("Failed to render event message")
	}

	log.Debug().
		Str("event_id", event.ID).
		Str("provider", provider).
		Str("rendered_template", renderedTemplate.String()).
		Msgf("Rendered %s template", mtype)

	return renderedTemplate.String()

}

// Build HTTP headers or params based on template
func renderHTTPKV(list []map[string]string, event models.Event, kvtype string, provider string) []map[string]string {
	var renderedList []map[string]string

	for _, item := range list {
		for k, v := range item {
			// Render
			tmpl, err := template.New("custom").Funcs(template.FuncMap{"env": includeenv}).Parse(v)
			if err != nil {
				log.Warn().Err(err).Msgf("Failed to render HTTP %s", kvtype)
			}

			var renderedTemplate bytes.Buffer
			err = tmpl.Execute(&renderedTemplate, event)
			if err != nil {
				log.Fatal().
					Err(err).
					Msgf("Failed to render HTTP %s", kvtype)
			}

			v = renderedTemplate.String()
			renderedList = append(renderedList, map[string]string{k: v})
		}
	}

	log.Debug().
		Str("event_id", event.ID).
		Str("provider", provider).
		Interface("rendered_template", renderedList).
		Msgf("Rendered HTTP %s template", kvtype)

	return renderedList
}

// includeenv retrieves environment variables for use within templates
func includeenv(env string) string {
	if strings.HasPrefix(env, "FN_") {
		value, ok := os.LookupEnv(env)
		if !ok {
			log.Warn().
				Msgf("Could not find matching env: %v", env)
			return ""
		}
		return value
	} else {
		log.Warn().
			Msg("Env vars used in templates must contain FN_ prefix")
		return ""

	}
}
