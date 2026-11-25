package notifier

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	"github.com/izerok/frigate-notify-plus/models"
)

// SamplePreviewEvent returns the fixed sample event used to preview message
// templates: a person at the front door, with GenAI data attached.
func SamplePreviewEvent() models.Event {
	event := models.Event{
		ID:           "preview",
		Camera:       "front_door",
		Label:        "person",
		SubLabel:     "",
		HasSnapshot:  true,
		HasClip:      true,
		StartTime:    float64(time.Now().Unix()),
		TopScore:     0.92,
		Zones:        []string{"yard"},
		CurrentZones: []string{"yard"},
	}
	event.Extra.GenAITitle = "有人在门口"
	event.Extra.GenAISummary = "门口站着一个人，正在按门铃。"
	event.Extra.GenAIThreatLevel = "Suspicious"
	event.Extra.GenAIObservations = []string{"一个人站在门口"}
	event.Extra.GenAIConfidence = "90%"
	return setExtras([]models.Event{event})
}

// RenderTemplatePreview renders a message template against the sample event.
// When text is non-empty it is rendered as a custom template; otherwise the
// named template (plaintext, markdown or html) is rendered. Unlike
// renderMessage, parse and execute failures are returned as errors instead
// of logging fatally.
func RenderTemplatePreview(name, text string) (string, error) {
	event := SamplePreviewEvent()

	var tmpl *template.Template
	var err error
	if text != "" {
		tmpl, err = template.New("custom").Funcs(template.FuncMap{"env": includeenv}).Parse(text)
		if err != nil {
			return "", fmt.Errorf("failed to parse custom template: %w", err)
		}
	} else {
		switch name {
		case "plaintext", "markdown", "html":
			tmpl, err = template.ParseFS(TemplateFiles, "templates/"+name+".template")
			if err != nil {
				return "", fmt.Errorf("failed to parse %s template: %w", name, err)
			}
		default:
			return "", fmt.Errorf("unknown template: %s", name)
		}
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, event); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return rendered.String(), nil
}
