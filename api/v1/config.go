package apiv1

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/events"
	"github.com/rs/zerolog/log"
)

type ConfigOutput struct {
	Body struct {
		Config   config.Config `json:"config"`
		Defaults config.Config `json:"defaults"`
	}
}

type PutConfigInput struct {
	Body struct {
		Config       config.Config `json:"config"`
		SkipSave     bool          `json:"skipsave,omitempty" doc:"Skip writing new config to file" default:"false"`
		SkipBackup   bool          `json:"skipbackup,omitempty" doc:"Skip creating config file backup" default:"false"`
		SkipValidate bool          `json:"skipvalidate,omitempty" doc:"Skip config validation checking"`
		SkipReload   bool          `json:"skipreload,omitempty" doc:"Skip config reload after updating settings" hidden:"true"`
	}
}

type PutConfigOutput struct {
	Body struct {
		Status string   `json:"status"`
		Errors []string `json:"errors,omitempty"`
	}
}

// GetConfig returns the current running configuration plus the factory
// defaults (config.DefaultConfig), so the Web UI can show "recommended"
// hints next to each setting.
func GetConfig(ctx context.Context, input *struct{}) (*ConfigOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/config").
		Str("method", "GET").
		Msg("Received API request")

	resp := &ConfigOutput{}
	resp.Body.Config = config.ConfigData
	resp.Body.Defaults = config.DefaultConfig

	log.Trace().
		Str("uri", V1_PREFIX+"/config").
		Interface("response_json", resp.Body).
		Msg("Sent API response")

	return resp, nil
}

// PutConfig replaces the current running configuration
func PutConfig(ctx context.Context, input *PutConfigInput) (*PutConfigOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/config").
		Str("method", "PUT").
		Msg("Received API request")

	resp := &PutConfigOutput{}

	newConfig := input.Body.Config

	var validationErrors []string
	if !input.Body.SkipValidate {
		log.Trace().Msg("Skipping config validation checks")
		validationErrors = newConfig.Validate()
	}

	if len(validationErrors) == 0 {
		resp.Body.Status = "ok"
		if !input.Body.SkipReload {
			go reloadCfg(newConfig, input.Body.SkipSave, input.Body.SkipBackup)
		}

		log.Trace().
			Str("uri", V1_PREFIX+"/config").
			Interface("response_json", resp.Body).
			Msg("Sent API response")
		return resp, nil
	} else {
		resp.Body.Status = "validation error"
		resp.Body.Errors = validationErrors

		log.Trace().
			Str("uri", V1_PREFIX+"/config").
			Interface("response_json", resp.Body).
			Msg("Sent API response")

		// Return the individual errors in the 422 body (huma serializes
		// them as errors[].message) so the Web UI can highlight the exact
		// field each error names, instead of showing one generic message.
		errs := make([]error, len(validationErrors))
		for i, msg := range validationErrors {
			errs[i] = errors.New(msg)
		}
		return resp, huma.NewError(422, "config validation failed", errs...)
	}
}

func reloadCfg(newconfig config.Config, skipSave bool, skipBackup bool) {
	log.Info().Msg("Reloading app config...")
	log.Trace().
		Bool("skipSave", skipSave).
		Bool("skipBackup", skipBackup).
		Msg("Config reload via API")
	if config.ConfigData.Frigate.MQTT.Enabled {
		events.DisconnectMQTT()
	}

	config.ConfigData = newconfig
	if !skipSave {
		config.Save(skipBackup)
	}

	if config.ConfigData.Frigate.MQTT.Enabled {
		events.SubscribeMQTT()
	}
	log.Info().Msg("Config reload completed")
}
