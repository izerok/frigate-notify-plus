package notifier

import (
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
)

func TestRecordHistoryCapsAt100(t *testing.T) {
	ClearHistory()
	for i := 0; i < 105; i++ {
		RecordHistory(models.Event{Camera: "cam", Label: "person"}, []string{"webhook"}, false, false)
	}
	got := History()
	if len(got) != maxHistoryEntries {
		t.Fatalf("History() len = %d, want %d", len(got), maxHistoryEntries)
	}
	// Newest first: the last recorded entry is at index 0.
	if len(got) > 1 && !got[0].Time.After(got[1].Time) && !got[0].Time.Equal(got[1].Time) {
		t.Error("History() is not newest-first")
	}
}

func TestRecordHistoryGenAIUpdateIsSeparateEntry(t *testing.T) {
	ClearHistory()
	initial := models.Event{Camera: "front_door", Label: "person", ID: "det-1"}
	initial.Extra.ReviewID = "rev-1"
	initial.Extra.GenAITitle = ""
	RecordHistory(initial, []string{"telegram"}, false, false)

	update := initial
	update.Extra.IsGenAIUpdate = true
	update.Extra.GenAITitle = "Person walks across the patio"
	RecordHistory(update, []string{"telegram"}, true, false)

	got := History()
	if len(got) != 2 {
		t.Fatalf("History() len = %d, want 2", len(got))
	}
	if !got[0].IsGenAIUpdate || got[0].GenAITitle != "Person walks across the patio" {
		t.Errorf("newest entry = %+v, want the GenAI update", got[0])
	}
	if got[1].IsGenAIUpdate || got[1].ReviewID != "rev-1" {
		t.Errorf("older entry = %+v, want the initial alert for rev-1", got[1])
	}
	if !got[0].OK || got[0].Detail == "" {
		t.Errorf("GenAI update entry should be marked ok with a detail, got %+v", got[0])
	}
}

func TestRecordHistoryLabelsAndTestFlag(t *testing.T) {
	ClearHistory()
	event := models.Event{Camera: "cam", Label: "car", SubLabel: "tesla", ID: "evt-1"}
	RecordHistory(event, []string{"webhook"}, false, true)
	got := History()
	if len(got) != 1 {
		t.Fatalf("History() len = %d, want 1", len(got))
	}
	if len(got[0].Labels) != 2 || got[0].Labels[0] != "car" || got[0].Labels[1] != "tesla" {
		t.Errorf("Labels = %v, want [car tesla]", got[0].Labels)
	}
	if !got[0].IsTest {
		t.Error("expected IsTest to be true")
	}
}
