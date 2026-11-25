package notifier

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// setupTwoWebhooks installs two enabled webhook profiles pointed at two mock
// receivers and returns their hit counters. Global config is restored on cleanup.
func setupTwoWebhooks(t *testing.T) (hits [2]*atomic.Int32) {
	t.Helper()
	var servers [2]*httptest.Server
	for i := range servers {
		hits[i] = &atomic.Int32{}
		h := hits[i]
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
	}

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
		notificationCooldown = cooldownTracker{}
	})
	for _, srv := range servers {
		t.Cleanup(srv.Close)
	}

	config.ConfigData = config.Config{Alerts: models.Alerts{
		Webhook: []models.Webhook{
			{AlertCommon: models.AlertCommon{Enabled: true}, Server: servers[0].URL, Method: "POST"},
			{AlertCommon: models.AlertCommon{Enabled: true}, Server: servers[1].URL, Method: "POST"},
		},
	}}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 2)
	notificationCooldown = cooldownTracker{}
	return hits
}

func waitForHits(t *testing.T, ctr *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ctr.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hits = %d, want >= %d", ctr.Load(), want)
}

var testAlertEvents = []models.Event{{ID: "evt1", Camera: "front_door", Label: "person"}}

func TestSendTestAlertToSingleProfile(t *testing.T) {
	hits := setupTwoWebhooks(t)

	SendTestAlertTo(testAlertEvents, "webhook", 1)

	waitForHits(t, hits[1], 1)
	// Give the (not expected) first profile a chance to fire before asserting it stayed silent.
	time.Sleep(300 * time.Millisecond)
	if got := hits[0].Load(); got != 0 {
		t.Errorf("profile 0 hits = %d, want 0", got)
	}
}

func TestSendTestAlertToProvider(t *testing.T) {
	hits := setupTwoWebhooks(t)

	SendTestAlertToProvider(testAlertEvents, "webhook")

	waitForHits(t, hits[0], 1)
	waitForHits(t, hits[1], 1)
}

func TestSendTestAlertHitsAll(t *testing.T) {
	hits := setupTwoWebhooks(t)

	SendTestAlert(testAlertEvents)

	waitForHits(t, hits[0], 1)
	waitForHits(t, hits[1], 1)
}

func TestSendTestAlertToUnknownTargetSendsNothing(t *testing.T) {
	hits := setupTwoWebhooks(t)

	sentinel := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	config.Internal.Status.LastNotification = sentinel

	SendTestAlertTo(testAlertEvents, "webhook", 5)
	SendTestAlertTo(testAlertEvents, "telegram", 0)
	SendTestAlertToProvider(testAlertEvents, "telegram")

	time.Sleep(300 * time.Millisecond)
	if got := hits[0].Load(); got != 0 {
		t.Errorf("profile 0 hits = %d, want 0", got)
	}
	if got := hits[1].Load(); got != 0 {
		t.Errorf("profile 1 hits = %d, want 0", got)
	}
	if !config.Internal.Status.LastNotification.Equal(sentinel) {
		t.Error("targeted test with no eligible sender must not stamp LastNotification")
	}
}
