package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/notifier"
)

func TestGetHistory(t *testing.T) {
	notifier.ClearHistory()
	defer notifier.ClearHistory()

	event := models.Event{Camera: "front_door", Label: "person", ID: "det-1"}
	event.Extra.ReviewID = "rev-1"
	event.Extra.GenAITitle = "Person walks across the patio"
	notifier.RecordHistory(event, []string{"telegram", "webhook"}, false, false)

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Get("/api/v1/history")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	var body struct {
		History []notifier.HistoryEntry `json:"history"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unable to decode history response: %v", err)
	}
	if len(body.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(body.History))
	}
	if body.History[0].Camera != "front_door" || body.History[0].GenAITitle != "Person walks across the patio" {
		t.Errorf("history entry = %+v", body.History[0])
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	notifier.ClearHistory()
	defer notifier.ClearHistory()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Get("/api/v1/history")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	var body struct {
		History []notifier.HistoryEntry `json:"history"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unable to decode history response: %v", err)
	}
	if body.History == nil || len(body.History) != 0 {
		t.Errorf("history = %v, want an empty array", body.History)
	}
}
