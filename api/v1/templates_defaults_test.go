package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/notifier"
)

func decodeTemplates(t *testing.T, respBody []byte) (string, string, string) {
	t.Helper()
	var body struct {
		Plaintext string `json:"plaintext"`
		Markdown  string `json:"markdown"`
		HTML      string `json:"html"`
	}
	if err := json.Unmarshal(respBody, &body); err != nil {
		t.Fatalf("Unable to decode templates response: %v", err)
	}
	return body.Plaintext, body.Markdown, body.HTML
}

func TestGetTemplateDefaults(t *testing.T) {
	// The overlay FS holds disk-edited templates; the embedded FS holds
	// the factory originals. The defaults endpoint must return the
	// factory originals, ignoring the edits.
	notifier.TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("edited plain")},
		"templates/markdown.template":  {Data: []byte("edited md")},
		"templates/html.template":      {Data: []byte("edited html")},
	}
	origEmbedded := notifier.EmbeddedTemplates
	notifier.EmbeddedTemplates = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("factory plain")},
		"templates/markdown.template":  {Data: []byte("factory md")},
		"templates/html.template":      {Data: []byte("factory html")},
	}
	t.Cleanup(func() { notifier.EmbeddedTemplates = origEmbedded })

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Get("/api/v1/templates/defaults")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	plain, md, html := decodeTemplates(t, resp.Body.Bytes())
	if plain != "factory plain" || md != "factory md" || html != "factory html" {
		t.Errorf("defaults = %q %q %q, want factory templates", plain, md, html)
	}

	// The plain endpoint still returns the edited (overlay) templates.
	resp = api.Get("/api/v1/templates")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	plain, md, html = decodeTemplates(t, resp.Body.Bytes())
	if plain != "edited plain" || md != "edited md" || html != "edited html" {
		t.Errorf("templates = %q %q %q, want edited templates", plain, md, html)
	}
}

func TestGetTemplateDefaultsFallback(t *testing.T) {
	// With EmbeddedTemplates unset (e.g. in tests), the defaults endpoint
	// falls back to TemplateFiles instead of failing.
	setupTemplatesTest(t)
	notifier.EmbeddedTemplates = nil

	_, api := humatest.New(t)
	Registerv1Routes(api)

	resp := api.Get("/api/v1/templates/defaults")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	plain, md, html := decodeTemplates(t, resp.Body.Bytes())
	if plain != "plain {{ .Label }}" || md != "md {{ .Label }}" || html != "<p>{{ .Label }}</p>" {
		t.Errorf("defaults = %q %q %q, want TemplateFiles content", plain, md, html)
	}
}
