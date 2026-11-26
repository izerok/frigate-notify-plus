package notifier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util"
)

// pushoverAPIEndpoint is the base URL of the Pushover API. It is a
// package-level variable so tests can point sends at a mock server.
var pushoverAPIEndpoint = "https://api.pushover.net/1"

// Limits and formats of the Pushover message API.
const (
	pushoverMessageMaxLength   = 1024
	pushoverTitleMaxLength     = 250
	pushoverURLMaxLength       = 512
	pushoverURLTitleMaxLength  = 100
	pushoverMaxAttachmentBytes = 2621440
	pushoverPriorityLowest     = -2
	pushoverPriorityEmergency  = 2
	pushoverPriorityMax        = 2
)

var (
	pushoverTokenRegexp  = regexp.MustCompile(`^[A-Za-z0-9]{30}$`)
	pushoverDeviceRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{1,25}$`)
)

// pushoverResponse is the JSON body the Pushover API answers with.
type pushoverResponse struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Errors  []string `json:"errors"`
}

// pushoverMessage carries everything one send needs, mirroring the
// fields the previous client library assembled from the profile and the
// event.
type pushoverMessage struct {
	Message    string
	Title      string
	Priority   int
	URL        string
	URLTitle   string
	Sound      string
	DeviceName string
	HTML       bool
	Retry      time.Duration // emergency priority only
	Expire     time.Duration // emergency priority only
	TTL        time.Duration
	Attachment io.Reader
}

// validate checks the tokens and message the same way the Pushover API
// (and the client library previously used here) does, with the same
// error texts, so a misconfigured profile fails before any request.
func (m *pushoverMessage) validate(token, userkey string) error {
	if token == "" {
		return errors.New("pushover: empty API token")
	}
	if !pushoverTokenRegexp.MatchString(token) {
		return errors.New("pushover: invalid API token")
	}
	if userkey == "" {
		return errors.New("pushover: empty recipient token")
	}
	if !pushoverTokenRegexp.MatchString(userkey) {
		return errors.New("pushover: invalid recipient token")
	}
	if m.Message == "" {
		return errors.New("pushover: message empty")
	}
	if utf8.RuneCountInString(m.Message) > pushoverMessageMaxLength {
		return errors.New("pushover: message too long")
	}
	if utf8.RuneCountInString(m.Title) > pushoverTitleMaxLength {
		return errors.New("pushover: message title too long")
	}
	if utf8.RuneCountInString(m.URL) > pushoverURLMaxLength {
		return errors.New("pushover: message URL too long")
	}
	if utf8.RuneCountInString(m.URLTitle) > pushoverURLTitleMaxLength {
		return errors.New("pushover: message URL title too long")
	}
	if m.URL == "" && m.URLTitle != "" {
		return errors.New("pushover: empty URL, URLTitle needs an URL")
	}
	if m.Priority > pushoverPriorityMax || m.Priority < pushoverPriorityLowest {
		return errors.New("pushover: invalid priority")
	}
	if m.Priority == pushoverPriorityEmergency && (m.Retry == 0 || m.Expire == 0) {
		return errors.New("pushover: missing emergency parameter")
	}
	if m.DeviceName != "" {
		for _, device := range strings.Split(m.DeviceName, ",") {
			if !pushoverDeviceRegexp.MatchString(device) {
				return errors.New("pushover: invalid device name")
			}
		}
	}
	return nil
}

// formFields flattens the message into the API's form fields. Optional
// fields are only included when set, matching the API contract.
func (m *pushoverMessage) formFields(token, userkey string) map[string]string {
	fields := map[string]string{
		"token":    token,
		"user":     userkey,
		"message":  m.Message,
		"priority": strconv.Itoa(m.Priority),
	}
	if m.Title != "" {
		fields["title"] = m.Title
	}
	if m.URL != "" {
		fields["url"] = m.URL
	}
	if m.URLTitle != "" {
		fields["url_title"] = m.URLTitle
	}
	if m.Sound != "" {
		fields["sound"] = m.Sound
	}
	if m.DeviceName != "" {
		fields["device"] = m.DeviceName
	}
	if m.HTML {
		fields["html"] = "1"
	}
	if m.Priority == pushoverPriorityEmergency {
		fields["retry"] = strconv.FormatFloat(m.Retry.Seconds(), 'f', -1, 64)
		fields["expire"] = strconv.FormatFloat(m.Expire.Seconds(), 'f', -1, 64)
	}
	if m.TTL != 0 {
		fields["ttl"] = strconv.FormatFloat(m.TTL.Seconds(), 'f', -1, 64)
	}
	return fields
}

// send posts the message to the Pushover API through the shared
// notification HTTP helper, so the profile's proxy setting applies like
// every other channel. With an attachment the body is multipart form
// data; without one it is a plain form-encoded POST.
func (m *pushoverMessage) send(profile models.Pushover) (*pushoverResponse, error) {
	fields := m.formFields(profile.Token, profile.Userkey)

	var body []byte
	var contentType string
	if m.Attachment != nil {
		attachment, err := io.ReadAll(m.Attachment)
		if err != nil {
			return nil, err
		}
		if len(attachment) > pushoverMaxAttachmentBytes {
			return nil, errors.New("pushover: message attachment is too large")
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("attachment", "attachment")
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(attachment); err != nil {
			return nil, err
		}
		for name, value := range fields {
			if err := writer.WriteField(name, value); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		body = buf.Bytes()
		contentType = writer.FormDataContentType()
	} else {
		values := url.Values{}
		for name, value := range fields {
			values.Set(name, value)
		}
		body = []byte(values.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	responseBody, err := util.HTTPPostProxied(pushoverAPIEndpoint+"/messages.json", false, profile.Proxy,
		body, "", map[string]string{"Content-Type": contentType})

	// The API answers errors with a JSON body naming them; surface those
	// even when the HTTP status already marked the request as failed.
	var response pushoverResponse
	parseErr := json.Unmarshal(responseBody, &response)
	if parseErr == nil && len(response.Errors) > 0 {
		return &response, fmt.Errorf("Errors:\n%s", strings.Join(response.Errors, "\n"))
	}
	if err != nil {
		return nil, err
	}
	if parseErr != nil {
		return nil, fmt.Errorf("pushover: could not parse API response: %w", parseErr)
	}
	if response.Status != 1 {
		return &response, fmt.Errorf("pushover: API returned status %d", response.Status)
	}
	return &response, nil
}

// SendPushoverMessage sends alert message through Pushover service
func SendPushoverMessage(event models.Event, snapshot io.Reader, provider notifMeta) {
	profile := config.ConfigData.Alerts.Pushover[provider.index]
	status := &config.Internal.Status.Notifications.Pushover[provider.index]

	// Build notification
	var message string
	if profile.Template != "" {
		message = renderMessage(profile.Template, event, "message", "Pushover")
	} else {
		message = renderMessage("html", event, "message", "Pushover")
		message = strings.ReplaceAll(message, "<br />", "")
	}

	// Create new message
	var title string
	if profile.Title != "" {
		title = renderMessage(profile.Title, event, "title", "pushover")
	} else {
		title = renderMessage(config.ConfigData.Alerts.General.Title, event, "title", "pushover")
	}
	notif := &pushoverMessage{
		Message:  message,
		Title:    title,
		Priority: pushoverPriority(profile, event),
		Sound:    profile.Sound,
		HTML:     true,
		TTL:      time.Duration(profile.TTL) * time.Second,
	}

	// Add links
	if event.Extra.ReviewLink != "" {
		notif.URL = event.Extra.ReviewLink
		notif.URLTitle = "Review Event"
	} else {
		notif.URL = event.Extra.EventLink
		notif.URLTitle = "View Clip"
	}

	// If emergency priority, set retry / expiration
	if notif.Priority == 2 {
		notif.Retry = time.Duration(profile.Retry) * time.Second
		notif.Expire = time.Duration(profile.Expire) * time.Second
	}

	// Add target devices if specified
	if profile.Devices != "" {
		notif.DeviceName = strings.ReplaceAll(profile.Devices, " ", "")
	}

	if event.HasSnapshot {
		notif.Attachment = snapshot
	}

	log.Trace().
		Str("message", notif.Message).
		Int("priority", notif.Priority).
		Interface("recipient", "--secret removed--").
		Int("provider_id", provider.index).
		Msg("Send Pushover alert")

	if err := notif.validate(profile.Token, profile.Userkey); err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Pushover").
			Int("provider_id", provider.index).
			Msgf("Unable to send alert: %v", err)
		status.NotifFailure(err.Error())
		return
	}

	// Send notification
	response, err := notif.send(profile)
	if response != nil {
		log.Trace().
			Interface("payload", response).
			Int("provider_id", provider.index).
			Msg("Pushover response")
	}
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Pushover").
			Int("provider_id", provider.index).
			Msgf("Unable to send alert: %v", err)
		status.NotifFailure(err.Error())
		return
	}

	log.Info().
		Str("event_id", event.ID).
		Str("provider", "Pushover").
		Int("provider_id", provider.index).
		Msgf("Alert sent")
	status.NotifSuccess()
}
