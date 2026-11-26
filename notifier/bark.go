package notifier

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util"
)

// defaultBarkServer is the official Bark push service (bark.day). An empty
// server in the profile means this default; self-hosted servers set their
// own URL.
const defaultBarkServer = "https://api.day.app"

// barkPayload is the JSON body POSTed to {server}/{device_key}. Only
// non-empty optional fields are sent. The device key travels in the URL
// path, never in the body.
type barkPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Sound string `json:"sound,omitempty"`
	Badge *int   `json:"badge,omitempty"`
	Icon  string `json:"icon,omitempty"`
	URL   string `json:"url,omitempty"`
	Group string `json:"group,omitempty"`
	Level string `json:"level,omitempty"`
}

// barkResponse is the JSON body Bark answers with. Success is code 200;
// anything else carries the reason in message.
type barkResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

func barkServer(profile models.Bark) string {
	server := strings.TrimRight(strings.TrimSpace(profile.Server), "/")
	if server == "" {
		server = defaultBarkServer
	}
	return server
}

// SendBark forwards an alert to a Bark server as an iOS push notification.
// The title and body are rendered through the same templates as every other
// provider, so GenAI fields flow through unchanged; Bark only delivers.
func SendBark(event models.Event, provider notifMeta) {
	profile := config.ConfigData.Alerts.Bark[provider.index]
	status := &config.Internal.Status.Notifications.Bark[provider.index]

	// Build notification
	var message string
	if profile.Template != "" {
		message = renderMessage(profile.Template, event, "message", "Bark")
	} else {
		message = renderMessage("plaintext", event, "message", "Bark")
	}
	var title string
	if profile.Title != "" {
		title = renderMessage(profile.Title, event, "title", "bark")
	} else {
		title = renderMessage(config.ConfigData.Alerts.General.Title, event, "title", "bark")
	}
	payload := barkPayload{
		Title: title,
		Body:  message,
		Sound: profile.Sound,
		Icon:  profile.Icon,
		URL:   profile.URL,
		Group: profile.Group,
		Level: profile.Level,
	}
	if profile.Badge != 0 {
		badge := profile.Badge
		payload.Badge = &badge
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Bark").
			Int("provider_id", provider.index).
			Err(err).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}

	// Bark answers HTTP 200 with {"code":200,...} on success. On an HTTP
	// error status util.HTTPPost returns only the status code (the JSON
	// body is lost), the same as the other senders; the JSON code is
	// checked below for the 200-with-error case.
	barkURL := fmt.Sprintf("%s/%s", barkServer(profile), profile.DeviceKey)
	header := map[string]string{"Content-Type": "application/json"}
	response, err := util.HTTPPostProxied(barkURL, false, profile.Proxy, data, "", header)
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Bark").
			Int("provider_id", provider.index).
			Err(err).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}
	var result barkResponse
	if err := json.Unmarshal(response, &result); err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Bark").
			Int("provider_id", provider.index).
			Str("response", string(response)).
			Msg("Unable to send alert: invalid Bark response")
		status.NotifFailure(fmt.Sprintf("invalid Bark response: %s", string(response)))
		return
	}
	if result.Code != 200 {
		msg := result.Message
		if msg == "" {
			msg = fmt.Sprintf("Bark returned code %d", result.Code)
		}
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Bark").
			Int("provider_id", provider.index).
			Str("error", msg).
			Msg("Unable to send alert")
		status.NotifFailure(msg)
		return
	}
	log.Info().
		Str("event_id", event.ID).
		Str("provider", "Bark").
		Int("provider_id", provider.index).
		Msg("Alert sent")
	status.NotifSuccess()
}
