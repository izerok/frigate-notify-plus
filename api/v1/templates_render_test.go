package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/notifier"
)

func setupRenderTemplatesTest(t *testing.T) {
	t.Helper()
	notifier.TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("plain {{ .Extra.GenAITitle }}")},
		"templates/markdown.template":  {Data: []byte("md {{ .Extra.GenAITitle }} - {{ .Label }}")},
		"templates/html.template":      {Data: []byte("<p>{{ .Extra.GenAITitle }}</p>")},
	}
}

func postRender(t *testing.T, api humatest.TestAPI, payload map[string]string) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp := api.Post("/api/v1/templates/render", bytes.NewReader(body))
	var decoded map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &decoded)
	return resp.Code, decoded
}

func TestPostRenderTemplate(t *testing.T) {
	setupRenderTemplatesTest(t)
	_, api := humatest.New(t)
	Registerv1Routes(api)

	t.Run("named markdown template contains the sample genai title", func(t *testing.T) {
		code, body := postRender(t, api, map[string]string{"name": "markdown"})
		if code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d: %v", code, body)
		}
		rendered, _ := body["rendered"].(string)
		if rendered == "" || !strings.Contains(rendered, "有人在门口") {
			t.Errorf("rendered = %q, want it to contain 有人在门口", rendered)
		}
		if !strings.Contains(rendered, "person") {
			t.Errorf("rendered = %q, want it to contain the sample label", rendered)
		}
	})

	t.Run("custom text renders", func(t *testing.T) {
		code, body := postRender(t, api, map[string]string{"name": "markdown", "text": "custom {{ .Camera }} {{ .Extra.GenAISummary }}"})
		if code != http.StatusOK {
			t.Fatalf("expected HTTP 200, got %d: %v", code, body)
		}
		rendered, _ := body["rendered"].(string)
		if rendered != "custom front_door 门口站着一个人，正在按门铃。" {
			t.Errorf("rendered = %q", rendered)
		}
	})

	t.Run("bad template is a 422", func(t *testing.T) {
		code, _ := postRender(t, api, map[string]string{"name": "markdown", "text": "{{ .Bad"})
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("expected HTTP 422 for a bad template, got %d", code)
		}
	})

	t.Run("unknown name is a 422", func(t *testing.T) {
		code, _ := postRender(t, api, map[string]string{"name": "nope"})
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("expected HTTP 422 for an unknown template, got %d", code)
		}
	})
}
