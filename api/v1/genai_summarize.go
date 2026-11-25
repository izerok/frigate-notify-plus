package apiv1

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/notifier"
)

// GenAISummarizeRunInput runs one scheduled GenAI summary immediately, for
// testing the schedule without waiting for its HH:MM.
type GenAISummarizeRunInput struct {
	Body struct {
		Index int `json:"index" doc:"Index of the schedule in alerts.general.genai.schedules"`
	}
}

type GenAISummarizeRunOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// RunGenAISchedule runs the configured schedule at index once, right now.
// It validates synchronously (unknown index, disabled schedule, GenAI off
// all return 422) and then runs the Frigate summarize + notify in a
// goroutine, so a slow Frigate call does not hold the HTTP request open.
func RunGenAISchedule(ctx context.Context, input *GenAISummarizeRunInput) (*GenAISummarizeRunOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/genai_summarize/run").
		Str("method", "POST").
		Msg("Received API request")

	schedules := config.ConfigData.Alerts.General.GenAI.Schedules
	if input.Body.Index < 0 || input.Body.Index >= len(schedules) {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("no GenAI schedule at index %d", input.Body.Index))
	}
	schedule := schedules[input.Body.Index]
	if !models.DerefBool(schedule.Enabled, true) {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("GenAI schedule %d is disabled", input.Body.Index))
	}
	if !config.ConfigData.Alerts.General.GenAI.Enabled {
		return nil, huma.Error422UnprocessableEntity("GenAI is not enabled")
	}

	go func() {
		if err := notifier.RunGenAISchedule(schedule); err != nil {
			log.Warn().
				Err(err).
				Int("schedule", input.Body.Index).
				Msg("Manual GenAI summary run failed")
		}
	}()

	resp := &GenAISummarizeRunOutput{}
	resp.Body.Status = "ok"
	return resp, nil
}
