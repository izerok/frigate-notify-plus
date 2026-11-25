package apiv1

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/notifier"
	"github.com/izerok/frigate-notify-plus/util"
	"github.com/rs/zerolog/log"
)

type NotifTestOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// NotifTestInput accepts an optional target. With no target the test goes to
// every enabled provider, exactly as before. With provider set (and index
// optionally set) it goes only to that provider's profiles.
//
// Index is a string because huma rejects pointers for query parameters, so a
// *int cannot express "omitted" (0 is a valid profile index); it is parsed
// with strconv.Atoi and a non-integer value is a 422.
type NotifTestInput struct {
	Provider string `query:"provider" doc:"Only test this provider (e.g. telegram). Omit to test all providers."`
	Index    string `query:"index" doc:"Profile index within the provider (0-based). Omit to test all profiles of the provider."`
}

// PostNotifTest collects most the recent event from Frigate & sends a test notification to configured providers
func PostNotifTest(ctx context.Context, input *NotifTestInput) (*NotifTestOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/notif_test").
		Str("method", "POST").
		Msg("Received API request")

	if input.Provider == "" && input.Index != "" {
		return nil, huma.Error422UnprocessableEntity("index requires a provider")
	}
	var index *int
	if input.Index != "" {
		n, err := strconv.Atoi(input.Index)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("index must be an integer")
		}
		index = &n
	}
	if input.Provider != "" {
		if err := validateNotifTestTarget(input.Provider, index); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
	}

	resp := &NotifTestOutput{}
	resp.Body.Message = "ok"

	go func() {
		log.Info().Msg("Received request to test notifications")

		// Query frigate API for most recent event
		var events []models.Event
		uri := "/api/events"
		params := "?include_thumbnails=0&limit=1"
		url := config.ConfigData.Frigate.Server + uri + params

		response, err := util.HTTPGet(url, config.ConfigData.Frigate.Insecure, "", config.ConfigData.Frigate.Headers...)
		if err != nil {
			log.Error().
				Err(err).
				Msgf("Cannot get event from %s", url)
			return
		}
		json.Unmarshal([]byte(response), &events)

		// Send test notification (targeted or global)
		if input.Provider != "" {
			if index != nil {
				notifier.SendTestAlertTo(events, input.Provider, *index)
			} else {
				notifier.SendTestAlertToProvider(events, input.Provider)
			}
		} else {
			notifier.SendTestAlert(events)
		}
	}()

	log.Trace().
		Str("uri", V1_PREFIX+"/notif_test").
		Interface("response_json", resp.Body).
		Msg("Sent API response")

	return resp, nil
}

// validateNotifTestTarget rejects a targeted test before any event is fetched:
// unknown provider, out-of-range index, or a target with no enabled profile.
// A disabled target is a 422 (not a silent no-op) so the Web UI can show the
// reason immediately instead of polling for a delivery that will never come.
func validateNotifTestTarget(provider string, index *int) error {
	var enabled []bool
	switch provider {
	case "apprise_api":
		for _, p := range config.ConfigData.Alerts.AppriseAPI {
			enabled = append(enabled, p.Enabled)
		}
	case "bark":
		for _, p := range config.ConfigData.Alerts.Bark {
			enabled = append(enabled, p.Enabled)
		}
	case "discord":
		for _, p := range config.ConfigData.Alerts.Discord {
			enabled = append(enabled, p.Enabled)
		}
	case "gotify":
		for _, p := range config.ConfigData.Alerts.Gotify {
			enabled = append(enabled, p.Enabled)
		}
	case "matrix":
		for _, p := range config.ConfigData.Alerts.Matrix {
			enabled = append(enabled, p.Enabled)
		}
	case "mattermost":
		for _, p := range config.ConfigData.Alerts.Mattermost {
			enabled = append(enabled, p.Enabled)
		}
	case "ntfy":
		for _, p := range config.ConfigData.Alerts.Ntfy {
			enabled = append(enabled, p.Enabled)
		}
	case "pushover":
		for _, p := range config.ConfigData.Alerts.Pushover {
			enabled = append(enabled, p.Enabled)
		}
	case "signal":
		for _, p := range config.ConfigData.Alerts.Signal {
			enabled = append(enabled, p.Enabled)
		}
	case "smtp":
		for _, p := range config.ConfigData.Alerts.SMTP {
			enabled = append(enabled, p.Enabled)
		}
	case "telegram":
		for _, p := range config.ConfigData.Alerts.Telegram {
			enabled = append(enabled, p.Enabled)
		}
	case "webhook":
		for _, p := range config.ConfigData.Alerts.Webhook {
			enabled = append(enabled, p.Enabled)
		}
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
	if len(enabled) == 0 {
		return fmt.Errorf("provider %s has no profiles configured", provider)
	}
	if index != nil {
		if *index < 0 || *index >= len(enabled) {
			return fmt.Errorf("provider %s has %d profile(s), index %d is out of range", provider, len(enabled), *index)
		}
		if !enabled[*index] {
			return fmt.Errorf("provider %s profile #%d is disabled", provider, *index)
		}
		return nil
	}
	for _, e := range enabled {
		if e {
			return nil
		}
	}
	return fmt.Errorf("provider %s has no enabled profile", provider)
}
