package apiv1

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/izerok/frigate-notify-plus/config"
)

func TestGetConfigYAML(t *testing.T) {
	origConfig := config.ConfigData
	defer func() { config.ConfigData = origConfig }()
	config.ConfigData.Frigate.Server = "http://192.0.2.10:5000"
	config.ConfigData.Frigate.PublicURL = "https://frigate.test"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/yaml", nil)
	rec := httptest.NewRecorder()
	GetConfigYAML(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "server: http://192.0.2.10:5000") {
		t.Errorf("YAML response missing frigate server, got:\n%s", body)
	}
	if !strings.Contains(body, "public_url: https://frigate.test") {
		t.Errorf("YAML response must use koanf-compatible keys (public_url), got:\n%s", body)
	}
}

func TestPutConfigYAMLInvalidYAML(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/yaml", strings.NewReader("frigate: [unclosed"))
	rec := httptest.NewRecorder()
	PutConfigYAML(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected HTTP 422 for invalid YAML, got %d", rec.Code)
	}
}

func TestPutConfigYAMLInvalidConfig(t *testing.T) {
	// Valid YAML, but no Frigate server and no polling method: validation fails.
	yamlBody := "app:\n  mode: reviews\nfrigate:\n  server: ''\nalerts:\n  general:\n    title: Test\n"
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/yaml", strings.NewReader(yamlBody))
	rec := httptest.NewRecorder()
	PutConfigYAML(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected HTTP 422 for an invalid config, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No Frigate server specified") {
		t.Errorf("Expected a frigate-server validation error, got: %s", rec.Body.String())
	}
}

func TestPutConfigYAMLValid(t *testing.T) {
	// A fake Frigate that answers the version check.
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte("0.18.2"))
			return
		}
		http.NotFound(w, r)
	}))
	defer frigate.Close()

	origConfig := config.ConfigData
	origFile := config.ConfigFile
	defer func() {
		config.ConfigData = origConfig
		config.ConfigFile = origFile
	}()
	tmpFile := filepath.Join(t.TempDir(), "config.yml")
	config.ConfigFile = tmpFile

	yamlBody := fmt.Sprintf(`app:
  mode: reviews
  api:
    enabled: true
    port: 8000
frigate:
  server: %s
  webapi:
    enabled: true
    interval: 30
alerts:
  general:
    title: Test Alert
  webhook:
    - enabled: true
      server: http://192.0.2.20/hook
`, frigate.URL)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/yaml", strings.NewReader(yamlBody))
	rec := httptest.NewRecorder()
	PutConfigYAML(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("Expected HTTP 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// The reload runs in a goroutine; the config file write proves it ran.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, err := os.ReadFile(tmpFile); err == nil && strings.Contains(string(data), "server: "+frigate.URL) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("config file was not written after PUT /api/v1/config/yaml")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
