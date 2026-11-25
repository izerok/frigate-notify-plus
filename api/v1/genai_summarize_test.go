package apiv1

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/notifier"
)

// setupGenAISummarizeRun installs a mock Frigate (summarize endpoint returns
// fixed text), one enabled webhook profile pointed at a capturing receiver,
// and one enabled schedule. Returns the received bodies channel.
func setupGenAISummarizeRun(t *testing.T) chan string {
	t.Helper()

	// Minimal templates so Bark's renderMessage calls work (in production
	// main.go installs the embedded templates; API tests must do it here).
	origTemplates := notifier.TemplateFiles
	notifier.TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("{{ .Extra.GenAITitle }} {{ .Extra.GenAISummary }}")},
	}
	t.Cleanup(func() { notifier.TemplateFiles = origTemplates })

	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "summary": "定时总结测试：白天一切正常，无可疑活动。"}`))
	}))
	t.Cleanup(frigate.Close)

	bodies := make(chan string, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		bodies <- string(body)
	}))
	t.Cleanup(hook.Close)

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
		notifier.ClearHistory()
	})
	config.ConfigData = config.Config{
		Frigate: models.Frigate{Server: frigate.URL},
		Alerts: models.Alerts{
			General: models.General{GenAI: models.GenAIConfig{
				Enabled: true,
				Schedules: []models.GenAISchedule{
					{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"},
					{Enabled: models.BoolPtr(false), Time: "07:00", Window: "last_11h"},
				},
			}},
			Webhook: []models.Webhook{
				{AlertCommon: models.AlertCommon{Enabled: true}, Server: hook.URL, Method: "POST"},
			},
		},
	}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 1)
	notifier.ClearHistory()
	return bodies
}

func TestPostGenAISummarizeRun(t *testing.T) {
	bodies := setupGenAISummarizeRun(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/genai_summarize/run", map[string]any{"index": 0})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", resp.Code, resp.Body.String())
	}

	select {
	case body := <-bodies:
		if !strings.Contains(body, "定时总结测试") {
			t.Errorf("webhook post body = %s, want it to contain 定时总结测试", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the scheduled summary notification")
	}
}

func TestPostGenAISummarizeRunErrors(t *testing.T) {
	setupGenAISummarizeRun(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	// Unknown index.
	resp := api.Post("/api/v1/genai_summarize/run", map[string]any{"index": 99})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("index 99: expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
	}
	// Disabled schedule.
	resp = api.Post("/api/v1/genai_summarize/run", map[string]any{"index": 1})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("disabled schedule: expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
	}
	// GenAI off.
	config.ConfigData.Alerts.General.GenAI.Enabled = false
	resp = api.Post("/api/v1/genai_summarize/run", map[string]any{"index": 0})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("GenAI off: expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
	}
}

// TestPostGenAISummarizeRunBarkTarget proves a scheduled summary reaches a
// Bark target too (dispatchSummary covers every enabled profile).
func TestPostGenAISummarizeRunBarkTarget(t *testing.T) {
	var hits atomic.Int32
	bark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	t.Cleanup(bark.Close)

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Bark
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Bark = origStatus
		notifier.ClearHistory()
	})
	// Reuse the summarize mock + schedule setup, then swap webhook for bark.
	_ = setupGenAISummarizeRun(t)
	config.ConfigData.Alerts.Webhook = nil
	config.ConfigData.Alerts.Bark = []models.Bark{
		{AlertCommon: models.AlertCommon{Enabled: true}, Server: bark.URL, DeviceKey: "sched-key"},
	}
	config.Internal.Status.Notifications.Bark = make([]models.NotifierStatus, 1)

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Post("/api/v1/genai_summarize/run", map[string]any{"index": 0})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", resp.Code, resp.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("bark hits = %d, want >= 1", hits.Load())
}
