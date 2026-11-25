package apiv1

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// setupNotifTest installs a mock Frigate server (returns one recent event) and
// two enabled webhook profiles pointed at mock receivers. Returns the hit
// counters. Global config is restored on cleanup.
func setupNotifTest(t *testing.T) (hits [2]*atomic.Int32) {
	t.Helper()

	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"evt1","camera":"front_door","label":"person","has_snapshot":false}]`))
	}))

	var hookServers [2]*httptest.Server
	for i := range hookServers {
		hits[i] = &atomic.Int32{}
		h := hits[i]
		hookServers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
	}

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
	})
	t.Cleanup(frigate.Close)
	for _, srv := range hookServers {
		t.Cleanup(srv.Close)
	}

	config.ConfigData = config.Config{
		Frigate: models.Frigate{Server: frigate.URL},
		Alerts: models.Alerts{
			Webhook: []models.Webhook{
				{AlertCommon: models.AlertCommon{Enabled: true}, Server: hookServers[0].URL, Method: "POST"},
				{AlertCommon: models.AlertCommon{Enabled: true}, Server: hookServers[1].URL, Method: "POST"},
			},
		},
	}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 2)
	return hits
}

func waitForHit(t *testing.T, ctr *atomic.Int32, want int32) {
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

func TestPostNotifTestTargeted(t *testing.T) {
	hits := setupNotifTest(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/notif_test?provider=webhook&index=1")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}

	waitForHit(t, hits[1], 1)
	time.Sleep(300 * time.Millisecond)
	if got := hits[0].Load(); got != 0 {
		t.Errorf("profile 0 hits = %d, want 0", got)
	}
}

func TestPostNotifTestProviderOnly(t *testing.T) {
	hits := setupNotifTest(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/notif_test?provider=webhook")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}

	waitForHit(t, hits[0], 1)
	waitForHit(t, hits[1], 1)
}

func TestPostNotifTestGlobal(t *testing.T) {
	hits := setupNotifTest(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/notif_test")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}

	waitForHit(t, hits[0], 1)
	waitForHit(t, hits[1], 1)
}

func TestPostNotifTestBadTarget(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"unknown provider", "/api/v1/notif_test?provider=nosuch"},
		{"index out of range", "/api/v1/notif_test?provider=webhook&index=9"},
		{"negative index", "/api/v1/notif_test?provider=webhook&index=-1"},
		{"non-integer index", "/api/v1/notif_test?provider=webhook&index=abc"},
		{"index without provider", "/api/v1/notif_test?index=1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupNotifTest(t)
			_, api := humatest.New(t)
			Registerv1Routes(api)

			resp := api.Post(tc.url)
			if resp.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestPostNotifTestDisabledProfile(t *testing.T) {
	hits := setupNotifTest(t)
	config.ConfigData.Alerts.Webhook[1].Enabled = false
	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/notif_test?provider=webhook&index=1")
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
	}

	time.Sleep(300 * time.Millisecond)
	if got := hits[1].Load(); got != 0 {
		t.Errorf("disabled profile hits = %d, want 0", got)
	}
}
