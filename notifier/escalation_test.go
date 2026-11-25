package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// setupEscalation installs two webhook profiles (one server each, posts
// counted on buffered channels) plus a routing rule that narrows normal
// alerts to webhook#0 and an escalation rule to webhook#1 at MinThreat 2.
// Global state is restored via t.Cleanup.
func setupEscalation(t *testing.T) (chan WebhookPayload, chan WebhookPayload) {
	t.Helper()
	mkServer := func(ch chan WebhookPayload) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var payload WebhookPayload
			_ = json.Unmarshal(body, &payload)
			w.WriteHeader(http.StatusOK)
			ch <- payload
		}))
	}
	ch0 := make(chan WebhookPayload, 10)
	ch1 := make(chan WebhookPayload, 10)
	srv0 := mkServer(ch0)
	srv1 := mkServer(ch1)
	t.Cleanup(srv0.Close)
	t.Cleanup(srv1.Close)

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
		ClearHistory()
		clearEscalatedRules()
		hourlyCap.reset()
		notificationCooldown = cooldownTracker{}
	})
	config.ConfigData = config.Config{Alerts: models.Alerts{
		General: models.General{Cooldown: 0, MaxSnapRetry: 1},
		Webhook: []models.Webhook{
			{AlertCommon: models.AlertCommon{Enabled: true}, Server: srv0.URL, Method: "POST"},
			{AlertCommon: models.AlertCommon{Enabled: true}, Server: srv1.URL, Method: "POST"},
		},
		Routing: models.Routing{
			Rules: []models.RoutingRule{{
				Name:    "front door to webhook 0",
				Cameras: []string{"front_door"},
				Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
			}},
			Escalation: []models.EscalationRule{{
				MinThreat: models.IntPtr(2),
				Targets:   []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(1)}},
			}},
		},
	}}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 2)
	ClearHistory()
	clearEscalatedRules()
	hourlyCap.reset()
	notificationCooldown = cooldownTracker{}
	return ch0, ch1
}

func awaitPayloads(t *testing.T, ch chan WebhookPayload, want int, what string) []WebhookPayload {
	t.Helper()
	var got []WebhookPayload
	deadline := time.After(5 * time.Second)
	for len(got) < want {
		select {
		case p := <-ch:
			got = append(got, p)
		case <-deadline:
			t.Fatalf("timed out waiting for %d %s posts, got %d", want, what, len(got))
		}
	}
	return got
}

func assertNoPayload(t *testing.T, ch chan WebhookPayload, what string) {
	t.Helper()
	select {
	case p := <-ch:
		t.Fatalf("unexpected %s post: %+v", what, p)
	case <-time.After(300 * time.Millisecond):
	}
}

func escalationEvent(threat string) models.Event {
	event := models.Event{ID: "evt-esc-1", Camera: "front_door", Label: "person"}
	event.Extra.ReviewID = "rev-esc-1"
	event.Extra.IsGenAIUpdate = true
	event.Extra.GenAITitle = "有人在门口"
	event.Extra.GenAISummary = "门口站着一个人，正在撬门。"
	event.Extra.GenAIThreatLevel = threat
	return event
}

func TestMatchingEscalationRules(t *testing.T) {
	orig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = orig; clearEscalatedRules() })
	config.ConfigData = config.Config{Alerts: models.Alerts{
		Routing: models.Routing{Escalation: []models.EscalationRule{
			{MinThreat: models.IntPtr(1), Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}}},
			{MinThreat: models.IntPtr(2), Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(1)}}},
		}},
	}}
	clearEscalatedRules()
	events := []models.Event{{ID: "e1"}}
	events[0].Extra.GenAIThreatLevel = "Suspicious" // level 1

	// Threat 1 matches only the MinThreat-1 rule.
	if got := matchingEscalationRules("rev-1", events); len(got) != 1 || got[0] != 0 {
		t.Fatalf("threat 1 matched %v, want [0]", got)
	}
	// Same (review, rule) pair never fires twice.
	if got := matchingEscalationRules("rev-1", events); len(got) != 0 {
		t.Fatalf("second call matched %v, want []", got)
	}
	// A later update with a still-higher threat fires the higher rule.
	events[0].Extra.GenAIThreatLevel = "Critical" // level 2
	if got := matchingEscalationRules("rev-1", events); len(got) != 1 || got[0] != 1 {
		t.Fatalf("threat 2 matched %v, want [1]", got)
	}
	// A different review is tracked separately.
	if got := matchingEscalationRules("rev-2", events); len(got) != 2 {
		t.Fatalf("other review matched %v, want [0 1]", got)
	}
}

func TestGenAIEscalationSendsFreshAlert(t *testing.T) {
	ch0, ch1 := setupEscalation(t)

	// Initial alert (threat 0): routing narrows it to webhook#0 only.
	initial := models.Event{ID: "evt-esc-1", Camera: "front_door", Label: "person"}
	initial.Extra.ReviewID = "rev-esc-1"
	SendAlert([]models.Event{initial})
	awaitPayloads(t, ch0, 1, "initial")
	assertNoPayload(t, ch1, "initial escalation")

	// GenAI update, threat 1: caption edit on webhook#0, no escalation.
	SendAlert([]models.Event{escalationEvent("Suspicious")})
	awaitPayloads(t, ch0, 1, "genai update")
	assertNoPayload(t, ch1, "threat-1 escalation")

	// GenAI update, threat 2: caption edit on webhook#0 AND a fresh alert
	// on webhook#1 carrying the GenAI title and Critical threat.
	SendAlert([]models.Event{escalationEvent("Critical")})
	awaitPayloads(t, ch0, 1, "critical update")
	esc := awaitPayloads(t, ch1, 1, "escalation")
	if esc[0].GenAI == nil || esc[0].GenAI.Title != "有人在门口" || esc[0].GenAI.ThreatLevel != "Critical" {
		t.Errorf("escalation payload genai = %+v, want title 有人在门口 / Critical", esc[0].GenAI)
	}

	// Same threat again: the (review, rule) pair already fired, no 2nd post.
	SendAlert([]models.Event{escalationEvent("Critical")})
	awaitPayloads(t, ch0, 1, "repeat update")
	assertNoPayload(t, ch1, "repeat escalation")

	// The escalation is its own history entry.
	found := false
	for _, e := range History() {
		if strings.Contains(e.Detail, "GenAI 升级提醒") {
			found = true
			if !e.OK || len(e.Providers) != 1 || e.Providers[0] != "webhook" {
				t.Errorf("escalation history entry = %+v", e)
			}
		}
	}
	if !found {
		t.Error("no history entry with detail GenAI 升级提醒")
	}
}
