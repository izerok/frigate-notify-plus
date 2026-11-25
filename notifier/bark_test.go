package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// barkRequest is what the mock receiver captured from a single delivery.
type barkRequest struct {
	path string
	body string
}

// setupBark installs a single Bark profile pointing at a mock receiver and
// returns the captured request plus the provider's status counters. The
// global config it mutates is restored via t.Cleanup.
func setupBark(t *testing.T, profile models.Bark, responseStatus int, responseBody string) (*barkRequest, *models.NotifierStatus) {
	t.Helper()
	// Minimal templates so SendBark's renderMessage calls work.
	origTemplates := TemplateFiles
	TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("{{ .Extra.GenAITitle }} {{ .Extra.GenAISummary }}")},
	}
	t.Cleanup(func() { TemplateFiles = origTemplates })

	got := &barkRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.path = r.URL.Path
		got.body = string(body)
		w.WriteHeader(responseStatus)
		w.Write([]byte(responseBody))
	}))
	t.Cleanup(srv.Close)

	origAlerts := config.ConfigData.Alerts.Bark
	origStatus := config.Internal.Status.Notifications.Bark
	origTitle := config.ConfigData.Alerts.General.Title
	t.Cleanup(func() {
		config.ConfigData.Alerts.Bark = origAlerts
		config.Internal.Status.Notifications.Bark = origStatus
		config.ConfigData.Alerts.General.Title = origTitle
	})
	config.ConfigData.Alerts.General.Title = "Frigate Alert"

	profile.Server = srv.URL
	config.ConfigData.Alerts.Bark = []models.Bark{profile}
	config.Internal.Status.Notifications.Bark = make([]models.NotifierStatus, 1)
	return got, &config.Internal.Status.Notifications.Bark[0]
}

// TestSendBarkPayload covers the request shape: the device key travels in
// the URL path, title/body are rendered through the templates, optional
// fields are only sent when set, and a code-200 response counts as sent.
func TestSendBarkPayload(t *testing.T) {
	profile := models.Bark{
		DeviceKey: "devicekey123",
		Sound:     "alarm.caf",
		Badge:     3,
		Group:     "frigate",
		Level:     "timeSensitive",
	}
	got, status := setupBark(t, profile, http.StatusOK, `{"code":200,"message":"success","timestamp":1700000000}`)
	event := models.Event{ID: "evt1", Camera: "front_door", Label: "person"}
	event.Extra.GenAITitle = "有人在门口"
	event.Extra.GenAISummary = "门口站着一个人。"
	event.Extra.GenAIThreatLevel = "Suspicious"

	SendBark(event, notifMeta{name: "bark", index: 0})

	if got.path != "/devicekey123" {
		t.Errorf("path = %q, want /devicekey123", got.path)
	}
	if status.Sent != 1 || status.Failed != 0 {
		t.Errorf("status sent=%d failed=%d, want 1/0", status.Sent, status.Failed)
	}
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Sound string `json:"sound"`
		Badge *int   `json:"badge"`
		Group string `json:"group"`
		Level string `json:"level"`
		Icon  string `json:"icon"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal([]byte(got.body), &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v\n%s", err, got.body)
	}
	if payload.Title == "" {
		t.Error("title is empty")
	}
	if !strings.Contains(payload.Body, "有人在门口") || !strings.Contains(payload.Body, "门口站着一个人") {
		t.Errorf("body = %q, want it to carry the GenAI title and summary", payload.Body)
	}
	if payload.Sound != "alarm.caf" || payload.Badge == nil || *payload.Badge != 3 ||
		payload.Group != "frigate" || payload.Level != "timeSensitive" {
		t.Errorf("optional fields = %+v", payload)
	}
	if payload.Icon != "" || payload.URL != "" {
		t.Errorf("unset optional fields must be omitted, got icon=%q url=%q", payload.Icon, payload.URL)
	}
	if strings.Contains(got.body, "devicekey123") {
		t.Error("device key must not appear in the JSON body")
	}
}

// TestSendBarkEmptyOptionals checks an all-default profile sends only
// title/body, with no badge/level/sound keys at all.
func TestSendBarkEmptyOptionals(t *testing.T) {
	got, status := setupBark(t, models.Bark{DeviceKey: "k"}, http.StatusOK, `{"code":200,"message":"success"}`)

	SendBark(models.Event{ID: "evt1"}, notifMeta{name: "bark", index: 0})

	if status.Sent != 1 {
		t.Errorf("status sent = %d, want 1", status.Sent)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(got.body), &raw); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	for _, key := range []string{"sound", "badge", "icon", "url", "group", "level"} {
		if _, ok := raw[key]; ok {
			t.Errorf("key %q must be omitted when unset", key)
		}
	}
}

// TestSendBarkFailure records a failed delivery when Bark answers code != 200
// (HTTP 200 with an error body), and when the HTTP request itself fails.
func TestSendBarkFailure(t *testing.T) {
	_, status := setupBark(t, models.Bark{DeviceKey: "bad-key"}, http.StatusOK, `{"code":400,"message":"invalid device key"}`)

	SendBark(models.Event{ID: "evt1"}, notifMeta{name: "bark", index: 0})

	if status.Sent != 0 || status.Failed != 1 {
		t.Errorf("status sent=%d failed=%d, want 0/1", status.Sent, status.Failed)
	}
	if !strings.Contains(status.LastError, "invalid device key") {
		t.Errorf("last_error = %q, want it to carry Bark's message", status.LastError)
	}
}

func TestSendBarkHTTPFailure(t *testing.T) {
	_, status := setupBark(t, models.Bark{DeviceKey: "k"}, http.StatusInternalServerError, `{"code":500,"message":"server error"}`)

	SendBark(models.Event{ID: "evt1"}, notifMeta{name: "bark", index: 0})

	if status.Sent != 0 || status.Failed != 1 {
		t.Errorf("status sent=%d failed=%d, want 0/1", status.Sent, status.Failed)
	}
	if status.LastError == "" {
		t.Error("expected last_error to be recorded")
	}
}
