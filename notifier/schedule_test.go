package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

func TestScheduleDue(t *testing.T) {
	s := models.GenAISchedule{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"}
	at := func(h, m int) time.Time {
		return time.Date(2026, 11, 22, h, m, 0, 0, time.Local)
	}
	today := at(20, 0).Format("2006-01-02")
	yesterday := at(20, 0).AddDate(0, 0, -1).Format("2006-01-02")

	if !scheduleDue(s, at(20, 0), "") {
		t.Error("20:00 schedule should fire at 20:00 when never fired")
	}
	if !scheduleDue(s, at(20, 0), yesterday) {
		t.Error("20:00 schedule should fire at 20:00 when last fired yesterday")
	}
	if scheduleDue(s, at(20, 0), today) {
		t.Error("20:00 schedule must not fire twice in one day")
	}
	if scheduleDue(s, at(20, 1), "") {
		t.Error("20:00 schedule must not fire at 20:01")
	}
	if scheduleDue(s, at(7, 0), "") {
		t.Error("20:00 schedule must not fire at 07:00")
	}
	if scheduleDue(models.GenAISchedule{Enabled: models.BoolPtr(false), Time: "20:00"}, at(20, 0), "") {
		t.Error("disabled schedule must never fire")
	}
	if scheduleDue(models.GenAISchedule{Enabled: models.BoolPtr(true), Time: "25:99"}, at(20, 0), "") {
		t.Error("schedule with an invalid time must never fire")
	}
	// Cross-day: a 07:00 schedule fires on the next morning after firing at 20:00.
	morning := models.GenAISchedule{Enabled: models.BoolPtr(true), Time: "07:00", Window: "last_11h"}
	if !scheduleDue(morning, time.Date(2026, 11, 23, 7, 0, 0, 0, time.Local), today) {
		t.Error("07:00 schedule should fire on the next morning (last fired is yesterday)")
	}
}

func TestScheduleWindow(t *testing.T) {
	now := time.Date(2026, 11, 22, 20, 0, 0, 0, time.Local)

	start, end := scheduleWindow(models.GenAISchedule{Window: "today"}, now)
	if !start.Equal(time.Date(2026, 11, 22, 0, 0, 0, 0, time.Local)) || !end.Equal(now) {
		t.Errorf("today window = [%v, %v], want [2026-11-22 00:00, %v]", start, end, now)
	}
	start, end = scheduleWindow(models.GenAISchedule{Window: ""}, now)
	if !start.Equal(time.Date(2026, 11, 22, 0, 0, 0, 0, time.Local)) {
		t.Errorf("empty window should default to today, got start %v", start)
	}
	start, end = scheduleWindow(models.GenAISchedule{Window: "last_11h"}, now)
	if !start.Equal(now.Add(-11*time.Hour)) || !end.Equal(now) {
		t.Errorf("last_11h window = [%v, %v], want [%v, %v]", start, end, now.Add(-11*time.Hour), now)
	}
	start, _ = scheduleWindow(models.GenAISchedule{Window: "bogus"}, now)
	if !start.Equal(time.Date(2026, 11, 22, 0, 0, 0, 0, time.Local)) {
		t.Errorf("invalid window should fall back to today, got start %v", start)
	}

	if got := scheduleWindowLabel(models.GenAISchedule{Window: "today"}); got != "今天" {
		t.Errorf("today label = %q, want 今天", got)
	}
	if got := scheduleWindowLabel(models.GenAISchedule{Window: "last_11h"}); got != "过去 11 小时" {
		t.Errorf("last_11h label = %q, want 过去 11 小时", got)
	}
}

func TestRunGenAISchedule(t *testing.T) {
	var summarizePath string
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		summarizePath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "summary": "定时总结测试：白天一切正常，无可疑活动。"}`))
	}))
	t.Cleanup(frigate.Close)

	posts := make(chan WebhookPayload, 4)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
		posts <- payload
	}))
	t.Cleanup(webhook.Close)

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
		ClearHistory()
		hourlyCap.reset()
	})
	config.ConfigData = config.Config{
		Frigate: models.Frigate{Server: frigate.URL},
		Alerts: models.Alerts{
			General: models.General{GenAI: models.GenAIConfig{Enabled: true}},
			Webhook: []models.Webhook{
				{AlertCommon: models.AlertCommon{Enabled: true}, Server: webhook.URL, Method: "POST"},
			},
		},
	}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 1)
	ClearHistory()
	hourlyCap.reset()

	if err := RunGenAISchedule(models.GenAISchedule{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"}); err != nil {
		t.Fatalf("RunGenAISchedule returned error: %v", err)
	}
	if !strings.HasPrefix(summarizePath, "/api/review/summarize/start/") || !strings.Contains(summarizePath, "/end/") {
		t.Errorf("Frigate summarize path = %q, want /api/review/summarize/start/<ts>/end/<ts>", summarizePath)
	}
	select {
	case p := <-posts:
		if p.GenAI == nil || !strings.Contains(p.GenAI.Summary, "定时总结测试") {
			t.Errorf("webhook post genai summary = %+v, want it to contain 定时总结测试", p.GenAI)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the scheduled summary notification")
	}
	found := false
	for _, e := range History() {
		if strings.Contains(e.Detail, "定时总结 今天") {
			found = true
		}
	}
	if !found {
		t.Error("no history entry with detail 定时总结 今天")
	}

	// GenAI off: the schedule refuses to run.
	config.ConfigData.Alerts.General.GenAI.Enabled = false
	if err := RunGenAISchedule(models.GenAISchedule{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"}); err == nil {
		t.Error("RunGenAISchedule with GenAI disabled should return an error")
	}
}
