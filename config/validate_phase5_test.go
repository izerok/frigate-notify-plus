package config

import (
	"strings"
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
)

// Phase 5 validation coverage: Bark, escalation rules, per-label min_score,
// the hourly cap, and GenAI schedules. These tests assert error counts (the
// same style as the existing validate tests) plus the field-path prefix of
// each message, which the Web UI uses to highlight the offending field.

func TestValidateBark(t *testing.T) {
	// Missing device_key errors.
	cfg := Config{}
	cfg.Alerts.Bark = []models.Bark{{AlertCommon: models.AlertCommon{Enabled: true}}}
	errs := cfg.validateBark(0)
	if len(errs) != 1 {
		t.Fatalf("missing device_key: expected 1 error, got %v", errs)
	}
	if !strings.HasPrefix(errs[0], "alerts.bark.0.device_key:") {
		t.Errorf("error = %q, want prefix alerts.bark.0.device_key:", errs[0])
	}

	// Empty server is fine: the default (https://api.day.app) is applied.
	cfg.Alerts.Bark[0].DeviceKey = "key123"
	errs = cfg.validateBark(0)
	if len(errs) != 0 {
		t.Fatalf("empty server + device_key: expected 0 errors, got %v", errs)
	}
	if cfg.Alerts.Bark[0].Server != "https://api.day.app" {
		t.Errorf("server = %q, want default https://api.day.app", cfg.Alerts.Bark[0].Server)
	}

	// A bad level errors. (Disabled profiles are skipped by Validate()'s
	// loop, which only calls validateBark for enabled profiles; the
	// per-profile validator itself always checks.)
	cfg.Alerts.Bark[0].Level = "loud"
	errs = cfg.validateBark(0)
	if len(errs) != 1 || !strings.HasPrefix(errs[0], "alerts.bark.0.level:") {
		t.Errorf("bad level: expected 1 alerts.bark.0.level error, got %v", errs)
	}
}

func TestValidateEscalationRules(t *testing.T) {
	cfg := Config{}
	// Unknown provider + out-of-range index, both path-prefixed.
	cfg.Alerts.Routing.Escalation = []models.EscalationRule{{
		MinThreat: models.IntPtr(2),
		Targets: []models.RoutingTarget{
			{Provider: "nope", Index: models.IntPtr(0)},
			{Provider: "webhook", Index: models.IntPtr(3)},
		},
	}}
	cfg.Alerts.Webhook = []models.Webhook{{AlertCommon: models.AlertCommon{Enabled: true}}}
	errs := cfg.validateRouting()
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %v", errs)
	}
	if !strings.HasPrefix(errs[0], "alerts.routing.escalation.0.targets.0.provider:") {
		t.Errorf("error 0 = %q", errs[0])
	}
	if !strings.HasPrefix(errs[1], "alerts.routing.escalation.0.targets.1.index:") {
		t.Errorf("error 1 = %q", errs[1])
	}

	// MinThreat out of range (0-2).
	cfg.Alerts.Routing.Escalation = []models.EscalationRule{{MinThreat: models.IntPtr(5), Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}}}}
	if errs := cfg.validateRouting(); len(errs) != 1 {
		t.Errorf("MinThreat 5: expected 1 error, got %v", errs)
	}

	// A valid rule passes.
	cfg.Alerts.Routing.Escalation = []models.EscalationRule{{MinThreat: models.IntPtr(1), Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(-1)}}}}
	if errs := cfg.validateRouting(); len(errs) != 0 {
		t.Errorf("valid escalation rule: expected 0 errors, got %v", errs)
	}
}

func TestValidateMinScores(t *testing.T) {
	cfg := Config{}
	cfg.Alerts.Webhook = []models.Webhook{{
		AlertCommon: models.AlertCommon{
			Enabled: true,
			Filters: models.AlertFilter{MinScore: map[string]float64{"person": 0.6, "car": 1.2}},
		},
	}}
	errs := cfg.validateMinScores()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !strings.HasPrefix(errs[0], "alerts.webhook.0.filters.min_score.car:") {
		t.Errorf("error = %q, want prefix alerts.webhook.0.filters.min_score.car:", errs[0])
	}

	cfg.Alerts.Webhook[0].Filters.MinScore["car"] = 0.8
	if errs := cfg.validateMinScores(); len(errs) != 0 {
		t.Errorf("valid min_score: expected 0 errors, got %v", errs)
	}
}

func TestValidateHourlyCap(t *testing.T) {
	cfg := Config{}
	cfg.Alerts.General.MaxNotificationsPerHour = -1
	if errs := cfg.validateAlertGeneral(); len(errs) != 1 {
		t.Errorf("cap -1: expected 1 error, got %v", errs)
	}
	cfg.Alerts.General.MaxNotificationsPerHour = 10
	if errs := cfg.validateAlertGeneral(); len(errs) != 0 {
		t.Errorf("cap 10: expected 0 errors, got %v", errs)
	}
}

func TestValidateGenAISchedules(t *testing.T) {
	cfg := Config{}
	cfg.Alerts.General.GenAI.Schedules = []models.GenAISchedule{
		{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"},
		{Enabled: models.BoolPtr(true), Time: "25:00", Window: "today"},
		{Enabled: models.BoolPtr(true), Time: "07:00", Window: "last_0h"},
		{Enabled: models.BoolPtr(true), Time: "07:00", Window: "last_200h"},
		{Enabled: models.BoolPtr(true), Time: "07:00", Window: "today", Targets: []models.RoutingTarget{{Provider: "nope", Index: models.IntPtr(0)}}},
	}
	cfg.Alerts.Webhook = []models.Webhook{{AlertCommon: models.AlertCommon{Enabled: true}}}
	errs := cfg.validateGenAISchedules()
	if len(errs) != 4 {
		t.Fatalf("expected 4 errors, got %v", errs)
	}
	want := []string{
		"alerts.general.genai.schedules.1.time:",
		"alerts.general.genai.schedules.2.window:",
		"alerts.general.genai.schedules.3.window:",
		"alerts.general.genai.schedules.4.targets.0.provider:",
	}
	for i, prefix := range want {
		if !strings.HasPrefix(errs[i], prefix) {
			t.Errorf("error %d = %q, want prefix %q", i, errs[i], prefix)
		}
	}
	// Disabled schedules are still validated (a disabled row with a broken
	// time/window fails PUT, prompting the user to fix or delete it).
	cfg.Alerts.General.GenAI.Schedules[1].Enabled = models.BoolPtr(false)
	if errs := cfg.validateGenAISchedules(); len(errs) != 4 {
		t.Errorf("disabled schedule: expected 4 errors, got %v", errs)
	}
}

func TestParseScheduleTimeAndWindow(t *testing.T) {
	if h, m, err := ParseScheduleTime("07:30"); err != nil || h != 7 || m != 30 {
		t.Errorf("ParseScheduleTime(07:30) = %d:%d, %v", h, m, err)
	}
	for _, bad := range []string{"24:00", "20:60", "abc", ""} {
		if _, _, err := ParseScheduleTime(bad); err == nil {
			t.Errorf("ParseScheduleTime(%q) should fail", bad)
		}
	}
	if isToday, hours, err := ParseScheduleWindow("last_11h"); err != nil || isToday || hours != 11 {
		t.Errorf("ParseScheduleWindow(last_11h) = %v, %d, %v", isToday, hours, err)
	}
	if isToday, hours, err := ParseScheduleWindow("today"); err != nil || !isToday || hours != 0 {
		t.Errorf("ParseScheduleWindow(today) = %v, %d, %v", isToday, hours, err)
	}
	for _, bad := range []string{"last_0h", "last_169h", "last_xh", "last_5", "week"} {
		if _, _, err := ParseScheduleWindow(bad); err == nil {
			t.Errorf("ParseScheduleWindow(%q) should fail", bad)
		}
	}
}

// TestPointerDefaultsNormalize covers the nil side of the pointer fields:
// a key absent from an old config file (or a YAML PUT that omits it) takes
// the documented default, while an explicit 0 / false is left alone. The
// explicit-value side through a JSON PUT is covered by
// TestPutConfigKeepsExplicitZeros in api/v1, which goes through huma.
func TestPointerDefaultsNormalize(t *testing.T) {
	cfg := Config{}
	cfg.Alerts.Webhook = []models.Webhook{{AlertCommon: models.AlertCommon{Enabled: true}, Server: "http://127.0.0.1:18081/hook"}}
	// Nil everywhere: defaults must be filled.
	cfg.Alerts.Routing.Rules = []models.RoutingRule{{
		Name: "nil rule", Targets: []models.RoutingTarget{{Provider: "webhook"}},
	}}
	cfg.Alerts.Routing.Escalation = []models.EscalationRule{{
		Targets: []models.RoutingTarget{{Provider: "webhook"}},
	}}
	cfg.Alerts.General.GenAI.Schedules = []models.GenAISchedule{{
		Time: "20:00", Window: "today", Targets: []models.RoutingTarget{{Provider: "webhook"}},
	}}
	// Explicit zeros: must survive Validate untouched.
	cfg.Alerts.Routing.Rules = append(cfg.Alerts.Routing.Rules, models.RoutingRule{
		Name: "zero rule", MinThreat: models.IntPtr(0),
		Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
	})
	cfg.Alerts.Routing.Escalation = append(cfg.Alerts.Routing.Escalation, models.EscalationRule{
		MinThreat: models.IntPtr(0),
		Targets:   []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
	})
	cfg.Alerts.General.GenAI.Schedules = append(cfg.Alerts.General.GenAI.Schedules, models.GenAISchedule{
		Enabled: models.BoolPtr(false), Time: "07:00", Window: "today",
		Targets: []models.RoutingTarget{{Provider: "webhook", Index: models.IntPtr(0)}},
	})
	if errs := cfg.Validate(); len(errs) != 0 {
		// Full Validate runs every validator; the minimal fixture above
		// only exercises normalization, so filter to routing/schedule
		// errors (there must be none — the fixture is valid there).
		for _, e := range errs {
			if strings.Contains(e, "routing") || strings.Contains(e, "schedules") {
				t.Fatalf("Validate() routing/schedule errors = %v", errs)
			}
		}
	}
	// Nil side: documented defaults.
	if got := models.DerefInt(cfg.Alerts.Routing.Rules[0].MinThreat, 99); got != -1 {
		t.Errorf("nil rule MinThreat = %d, want -1", got)
	}
	if got := models.DerefInt(cfg.Alerts.Routing.Rules[0].Targets[0].Index, 99); got != -1 {
		t.Errorf("nil rule target Index = %d, want -1", got)
	}
	if got := models.DerefInt(cfg.Alerts.Routing.Escalation[0].MinThreat, 99); got != 1 {
		t.Errorf("nil escalation MinThreat = %d, want 1", got)
	}
	if got := models.DerefInt(cfg.Alerts.Routing.Escalation[0].Targets[0].Index, 99); got != -1 {
		t.Errorf("nil escalation target Index = %d, want -1", got)
	}
	if got := models.DerefBool(cfg.Alerts.General.GenAI.Schedules[0].Enabled, false); got != true {
		t.Errorf("nil schedule Enabled = %v, want true", got)
	}
	if got := models.DerefInt(cfg.Alerts.General.GenAI.Schedules[0].Targets[0].Index, 99); got != -1 {
		t.Errorf("nil schedule target Index = %d, want -1", got)
	}
	// Explicit-zero side: untouched.
	if got := models.DerefInt(cfg.Alerts.Routing.Rules[1].MinThreat, 99); got != 0 {
		t.Errorf("explicit rule MinThreat 0 became %d", got)
	}
	if got := models.DerefInt(cfg.Alerts.Routing.Rules[1].Targets[0].Index, 99); got != 0 {
		t.Errorf("explicit rule target Index 0 became %d", got)
	}
	if got := models.DerefInt(cfg.Alerts.Routing.Escalation[1].MinThreat, 99); got != 0 {
		t.Errorf("explicit escalation MinThreat 0 became %d", got)
	}
	if got := models.DerefBool(cfg.Alerts.General.GenAI.Schedules[1].Enabled, true); got != false {
		t.Errorf("explicit schedule Enabled false became %v", got)
	}
}
