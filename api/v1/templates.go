package apiv1

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"github.com/danielgtaylor/huma/v2"
	"github.com/izerok/frigate-notify-plus/notifier"
	"github.com/rs/zerolog/log"
)

type TemplatesOutput struct {
	Body struct {
		Plaintext string `json:"plaintext"`
		Markdown  string `json:"markdown"`
		HTML      string `json:"html"`
	}
}

type PutTemplatesInput struct {
	Body map[string]string `json:"templates"`
}

type PutTemplatesOutput struct {
	Body struct {
		Status string   `json:"status"`
		Errors []string `json:"errors,omitempty"`
	}
}

// GetTemplates returns the current message templates (plaintext, markdown,
// html). Templates edited through the Web UI on disk win over the embedded
// copies; unedited templates come from the embedded files.
func GetTemplates(ctx context.Context, input *struct{}) (*TemplatesOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/templates").
		Str("method", "GET").
		Msg("Received API request")

	return readTemplates(notifier.TemplateFiles), nil
}

// GetTemplateDefaults returns the factory (embedded) message templates
// (plaintext, markdown, html), ignoring any edits saved to disk. It reads
// from notifier.EmbeddedTemplates, falling back to TemplateFiles when the
// embedded FS was never assigned (e.g. in tests).
func GetTemplateDefaults(ctx context.Context, input *struct{}) (*TemplatesOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/templates/defaults").
		Str("method", "GET").
		Msg("Received API request")

	src := notifier.EmbeddedTemplates
	if src == nil {
		src = notifier.TemplateFiles
	}
	return readTemplates(src), nil
}

func readTemplates(src fs.FS) *TemplatesOutput {
	resp := &TemplatesOutput{}
	for _, name := range notifier.TemplateNames() {
		data, err := fs.ReadFile(src, "templates/"+notifier.TemplateFileName(name))
		if err != nil {
			log.Warn().Err(err).Str("template", name).Msg("Unable to read message template")
			continue
		}
		switch name {
		case "plaintext":
			resp.Body.Plaintext = string(data)
		case "markdown":
			resp.Body.Markdown = string(data)
		case "html":
			resp.Body.HTML = string(data)
		}
	}
	return resp
}

// PutTemplates replaces message templates. Every submitted template is
// parsed before anything is written, so a bad template cannot break
// notifications. Unknown template names are rejected.
func PutTemplates(ctx context.Context, input *PutTemplatesInput) (*PutTemplatesOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/templates").
		Str("method", "PUT").
		Msg("Received API request")

	resp := &PutTemplatesOutput{}

	known := map[string]bool{}
	for _, name := range notifier.TemplateNames() {
		known[name] = true
	}
	var errs []string
	for name, content := range input.Body {
		if !known[name] {
			errs = append(errs, fmt.Sprintf("unknown template: %s", name))
			continue
		}
		if _, err := template.New(name).Parse(content); err != nil {
			errs = append(errs, fmt.Sprintf("template %s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		resp.Body.Status = "validation error"
		resp.Body.Errors = errs
		return resp, huma.Error422UnprocessableEntity("template validation failed")
	}

	if err := os.MkdirAll(notifier.TemplateDir, 0755); err != nil {
		resp.Body.Status = "error"
		resp.Body.Errors = []string{err.Error()}
		return resp, huma.Error500InternalServerError("unable to create template directory")
	}
	for name, content := range input.Body {
		path := filepath.Join(notifier.TemplateDir, notifier.TemplateFileName(name))
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			resp.Body.Status = "error"
			resp.Body.Errors = []string{err.Error()}
			return resp, huma.Error500InternalServerError("unable to write template file")
		}
		log.Info().Str("template", name).Str("path", path).Msg("Message template saved")
	}

	resp.Body.Status = "ok"
	return resp, nil
}

type RenderTemplateInput struct {
	Body struct {
		Name string `json:"name"`
		Text string `json:"text,omitempty"`
	}
}

type RenderTemplateOutput struct {
	Body struct {
		Rendered string `json:"rendered"`
	}
}

// PostRenderTemplate renders a message template against the sample event.
// With a non-empty text the text is rendered as a custom template;
// otherwise the named template (plaintext, markdown or html) is rendered.
// Parse/execute failures and unknown names are 422s.
func PostRenderTemplate(ctx context.Context, input *RenderTemplateInput) (*RenderTemplateOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/templates/render").
		Str("method", "POST").
		Msg("Received API request")

	rendered, err := notifier.RenderTemplatePreview(input.Body.Name, input.Body.Text)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &RenderTemplateOutput{}
	resp.Body.Rendered = rendered
	return resp, nil
}
