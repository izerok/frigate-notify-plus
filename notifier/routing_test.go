package notifier

import (
	"fmt"
	"testing"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// setupRouting installs two webhook profiles and one ntfy profile in the
// global config and returns the full eligible-sender list for them. Global
// config is restored on cleanup.
func setupRouting(t *testing.T) []alertSender {
	t.Helper()
	orig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = orig })
	config.ConfigData = config.Config{Alerts: models.Alerts{
		Webhook: []models.Webhook{
			{AlertCommon: models.AlertCommon{Enabled: true}},
			{AlertCommon: models.AlertCommon{Enabled: true}},
		},
		Ntfy: []models.Ntfy{
			{AlertCommon: models.AlertCommon{Enabled: true}},
		},
	}}
	return []alertSender{
		{name: "webhook", index: 0},
		{name: "webhook", index: 1},
		{name: "ntfy", index: 0},
	}
}

func senderKeys(senders []alertSender) []string {
	var out []string
	for _, s := range senders {
		out = append(out, fmt.Sprintf("%s#%d", s.name, s.index))
	}
	return out
}

func TestThreatLevel(t *testing.T) {
	tests := []struct {
		threat string
		want   int
	}{
		{"Critical", 2},
		{"Suspicious", 1},
		{"Normal", 0},
		{"", 0},
		{"weird", 0},
	}
	for _, tc := range tests {
		event := models.Event{}
		event.Extra.GenAIThreatLevel = tc.threat
		if got := threatLevel(event); got != tc.want {
			t.Errorf("threatLevel(%q) = %d, want %d", tc.threat, got, tc.want)
		}
		if wantGenAI := tc.threat != ""; hasGenAI(event) != wantGenAI {
			t.Errorf("hasGenAI(%q) = %v, want %v", tc.threat, hasGenAI(event), wantGenAI)
		}
	}
}

func TestApplyRouting(t *testing.T) {
	frontDoor := []models.Event{{Camera: "front_door", Label: "person"}}
	critical := []models.Event{{Camera: "front_door", Label: "person"}}
	critical[0].Extra.GenAIThreatLevel = "Critical"
	normal := []models.Event{{Camera: "front_door", Label: "person"}}
	normal[0].Extra.GenAIThreatLevel = "Normal"

	senders := setupRouting(t)

	t.Run("no rules keeps all senders", func(t *testing.T) {
		config.ConfigData.Alerts.Routing = models.Routing{}
		if got := applyRouting(senders, frontDoor); len(got) != 3 {
			t.Errorf("applyRouting with no rules = %v, want all 3 senders", senderKeys(got))
		}
	})

	t.Run("matching rule keeps only its target", func(t *testing.T) {
		config.ConfigData.Alerts.Routing = models.Routing{Rules: []models.RoutingRule{{
			Name:    "front door to webhook 0",
			Cameras: []string{"front_door"},
			Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
		}}}
		got := applyRouting(senders, frontDoor)
		if len(got) != 1 || got[0].name != "webhook" || got[0].index != 0 {
			t.Errorf("applyRouting = %v, want [webhook#0]", senderKeys(got))
		}
	})

	t.Run("no matching rule falls back to all senders", func(t *testing.T) {
		config.ConfigData.Alerts.Routing = models.Routing{Rules: []models.RoutingRule{{
			Name:    "other camera only",
			Cameras: []string{"other_cam"},
			Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
		}}}
		if got := applyRouting(senders, frontDoor); len(got) != 3 {
			t.Errorf("applyRouting with no match = %v, want all 3 senders (fallback)", senderKeys(got))
		}
	})

	t.Run("min threat filters by highest event threat", func(t *testing.T) {
		config.ConfigData.Alerts.Routing = models.Routing{Rules: []models.RoutingRule{{
			Name:      "critical to webhook 1",
			MinThreat: models.IntPtr(1),
			Targets:   []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(1)}},
		}}}
		got := applyRouting(senders, critical)
		if len(got) != 1 || got[0].name != "webhook" || got[0].index != 1 {
			t.Errorf("applyRouting (critical) = %v, want [webhook#1]", senderKeys(got))
		}
		// A Normal-threat event does not reach MinThreat 1: fallback to all.
		if got := applyRouting(senders, normal); len(got) != 3 {
			t.Errorf("applyRouting (normal) = %v, want all 3 senders (fallback)", senderKeys(got))
		}
	})

	t.Run("index -1 target matches all profiles of the provider", func(t *testing.T) {
		config.ConfigData.Alerts.Routing = models.Routing{Rules: []models.RoutingRule{{
			Name:    "front door to all webhooks",
			Cameras: []string{"front_door"},
			Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(-1)}},
		}}}
		got := applyRouting(senders, frontDoor)
		if len(got) != 2 || got[0].name != "webhook" || got[1].name != "webhook" {
			t.Errorf("applyRouting = %v, want [webhook#0 webhook#1]", senderKeys(got))
		}
	})
}

func TestRuleMatches(t *testing.T) {
	events := []models.Event{{
		Camera:       "front_door",
		Label:        "person",
		SubLabel:     "courier",
		Zones:        []string{"yard"},
		CurrentZones: []string{"porch"},
	}}
	events[0].Extra.GenAIThreatLevel = "Suspicious"

	cases := []struct {
		name string
		rule models.RoutingRule
		want bool
	}{
		{"empty rule matches everything", models.RoutingRule{MinThreat: models.IntPtr(-1)}, true},
		{"camera match", models.RoutingRule{Cameras: []string{"front_door"}, MinThreat: models.IntPtr(-1)}, true},
		{"camera mismatch", models.RoutingRule{Cameras: []string{"garage"}, MinThreat: models.IntPtr(-1)}, false},
		{"label match", models.RoutingRule{Labels: []string{"person"}, MinThreat: models.IntPtr(-1)}, true},
		{"label mismatch", models.RoutingRule{Labels: []string{"dog"}, MinThreat: models.IntPtr(-1)}, false},
		{"zone match via Zones", models.RoutingRule{Zones: []string{"yard"}, MinThreat: models.IntPtr(-1)}, true},
		{"zone match via CurrentZones", models.RoutingRule{Zones: []string{"porch"}, MinThreat: models.IntPtr(-1)}, true},
		{"zone mismatch", models.RoutingRule{Zones: []string{"driveway"}, MinThreat: models.IntPtr(-1)}, false},
		{"sublabel match", models.RoutingRule{Sublabels: []string{"courier"}, MinThreat: models.IntPtr(-1)}, true},
		{"sublabel mismatch", models.RoutingRule{Sublabels: []string{"fedex"}, MinThreat: models.IntPtr(-1)}, false},
		{"min threat met", models.RoutingRule{MinThreat: models.IntPtr(1)}, true},
		{"min threat not met", models.RoutingRule{MinThreat: models.IntPtr(2)}, false},
		{"all conditions must match", models.RoutingRule{Cameras: []string{"front_door"}, Labels: []string{"dog"}, MinThreat: models.IntPtr(-1)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleMatches(tc.rule, events); got != tc.want {
				t.Errorf("ruleMatches(%+v) = %v, want %v", tc.rule, got, tc.want)
			}
		})
	}
}
