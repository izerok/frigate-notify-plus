package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/notifier"
)

// setupMuteTest points config.ConfigFile at a temp dir so mute.json is
// isolated, and (re)loads the (empty) mute state. Global mute state is
// restored on cleanup.
func setupMuteTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origFile := config.ConfigFile
	config.ConfigFile = filepath.Join(dir, "config.yml")
	notifier.LoadMutes()
	t.Cleanup(func() {
		config.ConfigFile = origFile
		notifier.LoadMutes()
	})
}

func TestMuteAPI(t *testing.T) {
	setupMuteTest(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	// Empty list is an array, never null.
	resp := api.Get("/api/v1/mute")
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/mute: expected HTTP 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"mutes":[]`) {
		t.Errorf("GET /api/v1/mute with no mutes = %s, want an empty mutes array", resp.Body.String())
	}

	// Valid mute.
	payload, _ := json.Marshal(map[string]interface{}{"camera": "front_door", "minutes": 30})
	resp = api.Post("/api/v1/mute", bytes.NewReader(payload))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST /api/v1/mute: expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}
	var created struct {
		Status string `json:"status"`
		Camera string `json:"camera"`
		Until  string `json:"until"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unable to decode mute response: %v", err)
	}
	if created.Status != "ok" || created.Camera != "front_door" || created.Until == "" {
		t.Errorf("POST /api/v1/mute response = %+v", created)
	}

	// List shows the mute with a remaining time.
	resp = api.Get("/api/v1/mute")
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/mute: expected HTTP 200, got %d", resp.Code)
	}
	var listed struct {
		Mutes []struct {
			Camera           string `json:"camera"`
			Until            string `json:"until"`
			RemainingSeconds int64  `json:"remaining_seconds"`
		} `json:"mutes"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unable to decode mute list: %v", err)
	}
	if len(listed.Mutes) != 1 || listed.Mutes[0].Camera != "front_door" {
		t.Fatalf("mutes = %+v, want one front_door entry", listed.Mutes)
	}
	if listed.Mutes[0].RemainingSeconds <= 0 || listed.Mutes[0].RemainingSeconds > 1800 {
		t.Errorf("remaining_seconds = %d, want between 1 and 1800", listed.Mutes[0].RemainingSeconds)
	}

	// Invalid mutes are 422s.
	for _, tc := range []struct {
		name    string
		payload map[string]interface{}
	}{
		{"empty camera", map[string]interface{}{"camera": "", "minutes": 30}},
		{"zero minutes", map[string]interface{}{"camera": "front_door", "minutes": 0}},
		{"minutes too large", map[string]interface{}{"camera": "front_door", "minutes": 4321}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.payload)
			resp := api.Post("/api/v1/mute", bytes.NewReader(body))
			if resp.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}

	// DELETE without a camera is a 422.
	resp = api.Delete("/api/v1/mute")
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE /api/v1/mute without camera: expected HTTP 422, got %d: %s", resp.Code, resp.Body.String())
	}

	// DELETE removes the mute.
	resp = api.Delete("/api/v1/mute?camera=front_door")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("DELETE /api/v1/mute: expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}
	resp = api.Get("/api/v1/mute")
	if !strings.Contains(resp.Body.String(), `"mutes":[]`) {
		t.Errorf("GET /api/v1/mute after delete = %s, want an empty mutes array", resp.Body.String())
	}
}
