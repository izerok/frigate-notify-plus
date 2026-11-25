package config

import (
	"reflect"
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
	yml "gopkg.in/yaml.v3"
)

// TestYAMLRoundTrip proves the yaml tags on the config structs match the
// koanf keys: a config marshalled to YAML (as config.Save and the Web UI
// advanced editor do) must unmarshal back to the identical struct.
func TestYAMLRoundTrip(t *testing.T) {
	orig := DefaultConfig
	orig.Frigate.Server = "http://192.0.2.10:5000"
	orig.Frigate.PublicURL = "https://frigate.test"
	orig.Frigate.Username = "admin"
	orig.Frigate.Password = "s3cret"
	orig.Frigate.WebAPI.Enabled = true
	orig.Frigate.WebAPI.Interval = 15
	orig.Frigate.MQTT.Server = "mqtt.test"
	orig.Frigate.MQTT.Port = 1884
	orig.Frigate.Cameras.Exclude = []string{"garage"}
	orig.Alerts.General.Title = "Test Alert"
	orig.Alerts.General.Cooldown = 30
	orig.Alerts.General.CameraCooldown = map[string]int{"front_door": 60, "driveway": 120}
	orig.Alerts.General.GenAI.SummaryIdleTime = 120
	orig.Alerts.General.ThreatPriority = models.ThreatPriority{
		NtfyNormal:               2,
		NtfySuspicious:           3,
		NtfyCritical:             4,
		GotifyNormal:             4,
		GotifySuspicious:         7,
		GotifyCritical:           9,
		PushoverNormal:           -1,
		PushoverSuspicious:       0,
		PushoverCritical:         1,
		TelegramSilentNormal:     true,
		TelegramSilentSuspicious: false,
		TelegramSilentCritical:   false,
	}
	orig.Alerts.Routing = models.Routing{
		Rules: []models.RoutingRule{
			{
				Name:      "verify-route",
				Cameras:   []string{"front_door"},
				Labels:    []string{"person"},
				Zones:     []string{"yard"},
				Sublabels: []string{"courier"},
				MinThreat: models.IntPtr(1),
				Targets: []models.RoutingTarget{
					{Provider: "webhook", Index: models.IntPtr(0)},
					{Provider: "ntfy", Index: models.IntPtr(-1)},
				},
			},
		},
		// An explicit min_threat 0 / index 0 must survive the round-trip:
		// both are legitimate values, not "unset" (see models/pointers.go).
		Escalation: []models.EscalationRule{
			{
				MinThreat: models.IntPtr(0),
				Targets: []models.RoutingTarget{
					{Provider: "webhook", Index: models.IntPtr(0)},
				},
			},
		},
	}
	orig.Alerts.Quiet.Start = "02:30"
	orig.Alerts.Quiet.End = "05:45"
	orig.Alerts.Zones.Allow = []string{"porch"}
	orig.Alerts.Labels.MinScore = 50
	orig.Alerts.Labels.Block = []string{"dog"}
	orig.Alerts.Telegram = []models.Telegram{
		{
			AlertCommon: models.AlertCommon{
				Enabled: true,
				Title:   "Telegram",
				Filters: models.AlertFilter{Cameras: []string{"front_door"}},
			},
			ChatID:                123456,
			Token:                 "bot-token",
			SendClipAfterSnapshot: true,
			ClipMaxSize:           25,
		},
	}
	orig.Alerts.Webhook = []models.Webhook{
		{
			AlertCommon: models.AlertCommon{Enabled: true},
			Server:      "http://192.0.2.20/hook",
			Method:      "POST",
			// A map body template: this exercises Webhook's custom YAML
			// marshaler (the embedded AlertCommon.Template and the outer
			// Template field would otherwise collide on the "template" key).
			Template: map[string]interface{}{"text": "hello {{ .Label }}"},
		},
	}
	orig.Monitor.URL = "http://192.0.2.30/ping"

	data, err := yml.Marshal(&orig)
	if err != nil {
		t.Fatalf("Unable to marshal config to YAML: %v", err)
	}

	var back Config
	if err := yml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unable to unmarshal config from YAML: %v", err)
	}

	if !reflect.DeepEqual(orig, back) {
		t.Fatalf("YAML round-trip changed the config.\nMarshalled YAML:\n%s", string(data))
	}
}
