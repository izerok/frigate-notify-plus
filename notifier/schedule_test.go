package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
		if strings.HasPrefix(r.URL.Path, "/api/review/summarize") {
			_, _ = w.Write([]byte(`{"success": true, "summary": "定时总结测试：白天一切正常，无可疑活动。"}`))
			return
		}
		// A review with concerns: the schedule must ask Frigate to summarize.
		_, _ = w.Write([]byte(`[{"data":{"metadata":{"potential_threat_level":1,"other_concerns":[]}}}]`))
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)

	p := runScheduleAndCollect(t, posts)
	if !strings.HasPrefix(summarizePath, "/api/review/summarize/start/") || !strings.Contains(summarizePath, "/end/") {
		t.Errorf("Frigate summarize path = %q, want /api/review/summarize/start/<ts>/end/<ts>", summarizePath)
	}
	if p.GenAI == nil || !strings.Contains(p.GenAI.Summary, "定时总结测试") {
		t.Errorf("webhook post genai summary = %+v, want it to contain 定时总结测试", p.GenAI)
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

// setupScheduleTest installs the config/webhook/history fixtures shared by
// the scheduled-summary tests and returns the webhook post channel.
func setupScheduleTest(t *testing.T, frigateURL string) <-chan WebhookPayload {
	t.Helper()
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
		Frigate: models.Frigate{Server: frigateURL},
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
	return posts
}

func runScheduleAndCollect(t *testing.T, posts <-chan WebhookPayload) WebhookPayload {
	t.Helper()
	// Run the first configured schedule, like the in-process scheduler does;
	// the tests' fixture config has none, so use the default test schedule.
	schedules := config.ConfigData.Alerts.General.GenAI.Schedules
	if len(schedules) == 0 {
		schedules = []models.GenAISchedule{{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today"}}
	}
	if err := RunGenAISchedule(schedules[0]); err != nil {
		t.Fatalf("RunGenAISchedule returned error: %v", err)
	}
	select {
	case p := <-posts:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the scheduled summary notification")
	}
	return WebhookPayload{}
}

// TestRunGenAIScheduleDigest: a window with routine activity (no concerns)
// must not call Frigate's summarize endpoint (which would return its canned
// "No concerns were found" placeholder); instead the digest is assembled
// from the individual reviews' GenAI metadata, ordered by start time, with
// consecutive duplicate segments collapsed. The assembled-list path is what
// this test pins, so the chat leg is made to fail explicitly (a routed 500,
// like Frigate's own error body) — previously the mock answered the chat
// endpoint with the reviews array too, and the test silently passed
// through the chat response-parse failure instead.
func TestRunGenAIScheduleDigest(t *testing.T) {
	summarizeCalled := false
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat/completion") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"An error occurred while processing your request."}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/review/summarize") {
			summarizeCalled = true
			w.Write([]byte(`{"success":true,"summary":"No concerns were found during this time period."}`))
			return
		}
		w.Write([]byte(`[
			{"camera":"room","start_time":1700000300,"data":{"metadata":{"title":"一人在家中使用电脑","potential_threat_level":0,"other_concerns":null}}},
			{"camera":"room","start_time":1700000400,"data":{"metadata":{"title":"一人在家中使用电脑","potential_threat_level":0,"other_concerns":null}}},
			{"camera":"kitchen","start_time":1700000100,"data":{"metadata":{"title":"有人进出厨房","potential_threat_level":0,"other_concerns":[]}}},
			{"camera":"hall","start_time":1700000200,"data":{"metadata":null}}
		]`))
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)

	p := runScheduleAndCollect(t, posts)

	if summarizeCalled {
		t.Error("summarize endpoint called for a window without concerns")
	}
	if p.GenAI == nil {
		t.Fatal("webhook post has no genai data")
	}
	summary := p.GenAI.Summary
	if got := strings.Count(summary, "一人在家中使用电脑"); got != 1 {
		t.Errorf("digest = %q, want the duplicated room segment collapsed to one line", summary)
	}
	if !strings.Contains(summary, "有人进出厨房") {
		t.Errorf("digest = %q, want it to contain 有人进出厨房", summary)
	}
	if strings.Index(summary, "有人进出厨房") > strings.Index(summary, "一人在家中使用电脑") {
		t.Errorf("digest = %q, want the 1700000100 activity before the 1700000300 one", summary)
	}
}

// TestRunDueSchedulesBlockedNeighbour: two schedules due in the same tick
// run concurrently. The first review fetch blocks until released; the
// neighbouring schedule must still fire and deliver, and both must be
// marked fired at dispatch time. (Sequential dispatch would leave the
// second schedule waiting behind the blocked run — and in the real
// one-minute tick loop, past its minute entirely.)
func TestRunDueSchedulesBlockedNeighbour(t *testing.T) {
	var reviewCalls atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseBlocked := func() { releaseOnce.Do(func() { close(release) }) }
	// Unblock the stuck fetch before the mock server is closed, even on
	// failure, so cleanup cannot hang.
	t.Cleanup(releaseBlocked)
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/chat/completion"):
			w.Write([]byte(`{"message":{"role":"assistant","content":"今天家中正常。"},"finish_reason":"stop"}`))
		case strings.HasPrefix(r.URL.Path, "/api/review/summarize"):
			w.Write([]byte(`{"success":true,"summary":"不应走 summarize"}`))
		default:
			// The first review fetch blocks; any later one answers at once.
			if reviewCalls.Add(1) == 1 {
				<-release
			}
			w.Write([]byte(benignReviewsJSON))
		}
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)

	now := time.Date(2026, 11, 25, 20, 0, 0, 0, time.Local)
	due := now.Format("15:04")
	config.ConfigData.Alerts.General.GenAI.Schedules = []models.GenAISchedule{
		{Enabled: models.BoolPtr(true), Time: due, Window: "today"},
		{Enabled: models.BoolPtr(true), Time: due, Window: "today"},
	}
	lastFired := make(map[int]string)
	runDueSchedules(now, lastFired)

	// Both schedules are marked fired at dispatch time.
	if lastFired[0] != "2026-11-25" || lastFired[1] != "2026-11-25" {
		t.Errorf("lastFired = %v, want both schedules marked 2026-11-25 at dispatch", lastFired)
	}

	// One run is blocked in its review fetch; the other must still deliver.
	select {
	case <-posts:
	case <-time.After(5 * time.Second):
		t.Fatal("no schedule delivered while a neighbouring run was blocked")
	}

	// After the block is released, the stuck schedule delivers too.
	releaseBlocked()
	select {
	case <-posts:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked schedule did not deliver after being released")
	}
}

// TestRunGenAIScheduleQuiet: a window with no reviews at all gets a short
// quiet-period line, not Frigate's placeholder.
func TestRunGenAIScheduleQuiet(t *testing.T) {
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)

	p := runScheduleAndCollect(t, posts)

	if p.GenAI == nil {
		t.Fatal("webhook post has no genai data")
	}
	if p.GenAI.Summary != "今天没有检测到活动" {
		t.Errorf("summary = %q, want 今天没有检测到活动", p.GenAI.Summary)
	}
}

// TestRunGenAIScheduleReviewCheckFailed: when the review list cannot be
// fetched the schedule falls back to Frigate's summarize endpoint, so a
// review-API hiccup does not drop the digest.
func TestRunGenAIScheduleReviewCheckFailed(t *testing.T) {
	summarizeCalled := false
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/review/summarize") {
			summarizeCalled = true
			w.Write([]byte(`{"success":true,"summary":"定时总结测试：白天一切正常。"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false}`))
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)

	p := runScheduleAndCollect(t, posts)

	if !summarizeCalled {
		t.Error("summarize endpoint not called when the review list is unavailable")
	}
	if p.GenAI == nil || !strings.Contains(p.GenAI.Summary, "定时总结测试") {
		t.Errorf("webhook post genai summary = %+v, want it to contain 定时总结测试", p.GenAI)
	}
}
