package apiv1

import (
	"context"

	"github.com/izerok/frigate-notify-plus/notifier"
	"github.com/rs/zerolog/log"
)

type HistoryOutput struct {
	Body struct {
		History []notifier.HistoryEntry `json:"history"`
	}
}

// GetHistory returns the recent dispatched notifications (newest first).
// With a data directory set, history is loaded from $DATA_DIR/history.jsonl
// at startup and survives restarts; without one it is in-memory only.
func GetHistory(ctx context.Context, input *struct{}) (*HistoryOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/history").
		Str("method", "GET").
		Msg("Received API request")

	resp := &HistoryOutput{}
	resp.Body.History = notifier.History()
	if resp.Body.History == nil {
		resp.Body.History = []notifier.HistoryEntry{}
	}

	log.Trace().
		Str("uri", V1_PREFIX+"/history").
		Int("entries", len(resp.Body.History)).
		Msg("Sent API response")

	return resp, nil
}
