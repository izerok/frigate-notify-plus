package notifier

import (
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
)

func TestCheckAlertFiltersMinScore(t *testing.T) {
	provider := notifMeta{name: "webhook", index: 0}
	filters := models.AlertFilter{MinScore: map[string]float64{"person": 0.6, "car": 0.8}}

	// One scored detection above its minimum passes.
	if !checkAlertFilters([]models.Event{{Label: "person", TopScore: 0.9}}, filters, provider) {
		t.Error("person at 0.9 should pass min_score 0.6")
	}
	// Every scored detection of a configured label below its minimum drops.
	if checkAlertFilters([]models.Event{{Label: "person", TopScore: 0.5}}, filters, provider) {
		t.Error("person at 0.5 should be dropped by min_score 0.6")
	}
	// One above and one below: the label passes (not every detection is below).
	if !checkAlertFilters([]models.Event{{Label: "person", TopScore: 0.5}, {Label: "person", TopScore: 0.7}}, filters, provider) {
		t.Error("person with one detection at 0.7 should pass min_score 0.6")
	}
	// A label without a configured minimum is unaffected, even at a low score.
	if !checkAlertFilters([]models.Event{{Label: "dog", TopScore: 0.1}}, filters, provider) {
		t.Error("dog has no min_score configured and should pass")
	}
	// An event of a configured label whose sibling label is below its own
	// minimum drops: the car detection is below 0.8.
	if checkAlertFilters([]models.Event{{Label: "person", TopScore: 0.9}, {Label: "car", TopScore: 0.5}}, filters, provider) {
		t.Error("car at 0.5 should drop the alert even though person passes")
	}
	// Detections without a score fail open (TopScore 0: audio-only events,
	// test alerts, Frigate list items that carry no score).
	if !checkAlertFilters([]models.Event{{Label: "person", TopScore: 0}}, filters, provider) {
		t.Error("person with no score should fail open, not be dropped")
	}
	// An unscored detection does not hide a scored one that is below minimum.
	if checkAlertFilters([]models.Event{{Label: "person", TopScore: 0}, {Label: "person", TopScore: 0.5}}, filters, provider) {
		t.Error("person with a scored detection at 0.5 should be dropped despite an unscored sibling")
	}
	// No min_score configured at all: everything passes as before.
	if !checkAlertFilters([]models.Event{{Label: "person", TopScore: 0.1}}, models.AlertFilter{}, provider) {
		t.Error("no min_score configured should pass")
	}
}
