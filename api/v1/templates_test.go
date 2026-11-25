package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/notifier"
)

func setupTemplatesTest(t *testing.T) {
	t.Helper()
	notifier.TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("plain {{ .Label }}")},
		"templates/markdown.template":  {Data: []byte("md {{ .Label }}")},
		"templates/html.template":      {Data: []byte("<p>{{ .Label }}</p>")},
	}
	dir := t.TempDir()
	origDir := notifier.TemplateDir
	notifier.TemplateDir = dir
	t.Cleanup(func() { notifier.TemplateDir = origDir })
}

func TestGetTemplates(t *testing.T) {
	setupTemplatesTest(t)

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Get("/api/v1/templates")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	var body struct {
		Plaintext string `json:"plaintext"`
		Markdown  string `json:"markdown"`
		HTML      string `json:"html"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unable to decode templates response: %v", err)
	}
	if body.Plaintext != "plain {{ .Label }}" || body.Markdown != "md {{ .Label }}" || body.HTML != "<p>{{ .Label }}</p>" {
		t.Errorf("templates = %+v", body)
	}
}

func TestPutTemplates(t *testing.T) {
	setupTemplatesTest(t)

	_, api := humatest.New(t)
	Registerv1Routes(api)

	payload, _ := json.Marshal(map[string]string{
		"markdown": "updated {{ .Label }} at {{ .Camera }}",
	})
	resp := api.Put("/api/v1/templates", bytes.NewReader(payload))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("Expected HTTP 202, got %d: %s", resp.Code, resp.Body.String())
	}
	written, err := os.ReadFile(filepath.Join(notifier.TemplateDir, "markdown.template"))
	if err != nil {
		t.Fatalf("Unable to read written template: %v", err)
	}
	if string(written) != "updated {{ .Label }} at {{ .Camera }}" {
		t.Errorf("written template = %q", string(written))
	}

	// A bad template must be rejected and must not touch the disk.
	bad, _ := json.Marshal(map[string]string{"plaintext": "unclosed {{ .Label"})
	resp = api.Put("/api/v1/templates", bytes.NewReader(bad))
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected HTTP 422 for an invalid template, got %d", resp.Code)
	}
	if _, err := os.Stat(filepath.Join(notifier.TemplateDir, "plaintext.template")); !os.IsNotExist(err) {
		t.Error("invalid template must not be written to disk")
	}

	// An unknown template name must be rejected.
	unknown, _ := json.Marshal(map[string]string{"nope": "x"})
	resp = api.Put("/api/v1/templates", bytes.NewReader(unknown))
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("Expected HTTP 422 for an unknown template, got %d", resp.Code)
	}
}
