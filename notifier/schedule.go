package notifier

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// scheduleWindow returns the [start, end] the schedule's summary covers.
// "today" starts at local midnight; "last_Nh" starts N hours back. Invalid
// values (rejected by config validation on PUT) fall back to today.
func scheduleWindow(s models.GenAISchedule, now time.Time) (time.Time, time.Time) {
	if isToday, hours, err := config.ParseScheduleWindow(s.Window); err == nil && !isToday {
		return now.Add(-time.Duration(hours) * time.Hour), now
	}
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), now
}

// scheduleDue reports whether schedule s should fire at now, given the date
// (YYYY-MM-DD) it last fired on. A disabled schedule never fires; otherwise
// it fires when the clock matches its HH:MM and it has not fired today yet.
// The last-fired date is the only state, kept in memory: a restart may let a
// schedule fire once more the same day.
func scheduleDue(s models.GenAISchedule, now time.Time, lastFired string) bool {
	if !models.DerefBool(s.Enabled, true) {
		return false
	}
	hour, minute, err := config.ParseScheduleTime(s.Time)
	if err != nil {
		return false
	}
	if now.Hour() != hour || now.Minute() != minute {
		return false
	}
	return lastFired != now.Format("2006-01-02")
}

// scheduleWindowLabel is the short human-readable window name recorded in
// history and shown in the UI, e.g. "今天" or "过去 11 小时".
func scheduleWindowLabel(s models.GenAISchedule) string {
	if isToday, hours, err := config.ParseScheduleWindow(s.Window); err == nil {
		if isToday {
			return "今天"
		}
		return fmt.Sprintf("过去 %d 小时", hours)
	}
	return s.Window
}

// RunGenAISchedule executes one scheduled summary now and sends the text to
// the schedule's targets (all enabled channels when targets is empty).
// Unlike the idle activity summary, a scheduled digest reports even a quiet
// period — a window with nothing suspicious still gets a "what happened"
// digest. Returns an error when GenAI is off, Frigate fails, or the summary
// is empty.
func RunGenAISchedule(s models.GenAISchedule) error {
	if !config.ConfigData.Alerts.General.GenAI.Enabled {
		return fmt.Errorf("GenAI is not enabled")
	}
	now := time.Now()
	start, end := scheduleWindow(s, now)
	summary, err := scheduleSummary(s, start, end)
	if err != nil {
		return err
	}
	if summary == "" {
		return fmt.Errorf("Frigate returned an empty summary")
	}
	label := "定时总结 " + scheduleWindowLabel(s)
	dispatchSummary(summaryEvent(summary, start), s.Targets, label)
	return nil
}

// scheduleSummary produces the digest text for the window. It asks
// Frigate's GenAI chat (tool calling, the provider configured in Frigate)
// to summarize the window, attaching the per-event list as context so a
// small local model does not have to fetch it itself. The schedule's
// configured prompt, when set, controls the wording. When the chat call
// fails the digest falls back to Frigate's summarize endpoint (concern
// windows) or the per-event list. A window with no reviews at all gets a
// quiet-period line.
func scheduleSummary(s models.GenAISchedule, start, end time.Time) (string, error) {
	reviews, err := windowReviews(start, end)
	if err != nil {
		// Without the review list we cannot tell concerns from routine
		// activity, so fall back to Frigate's summarize endpoint directly.
		return requestFrigateSummary(start, end)
	}
	if len(reviews) == 0 {
		return scheduleWindowLabel(s) + "没有检测到活动", nil
	}
	lines := digestLines(reviews)
	if len(lines) > 0 {
		prompt := s.Prompt
		if prompt == "" {
			prompt = defaultDigestPrompt
		}
		prompt += "\n\n事件列表（时间 相机: 描述）：\n" + digestRender(lines, digestPromptMaxLines)
		if summary, err := frigateChat(prompt); err != nil {
			log.Warn().Err(err).Msg("Frigate chat summary failed, falling back")
		} else {
			return summary, nil
		}
	}
	if reviewsHaveConcerns(reviews) {
		return requestFrigateSummary(start, end)
	}
	if len(lines) == 0 {
		return fmt.Sprintf("%s检测到 %d 条活动（无 GenAI 描述）", scheduleWindowLabel(s), len(reviews)), nil
	}
	return digestRender(lines, digestMaxLines), nil
}

// defaultDigestPrompt is used when a schedule has no custom prompt.
const defaultDigestPrompt = "请用简体中文总结下面事件列表里这段时间家里发生了什么：按时间顺序概述，把同类或连续的活动合并（例如多次进出、持续的工作或休息），点出值得注意的情况。不要逐条罗列，不要编造列表中没有的信息，总长度控制在200字以内。"

const digestMaxLines = 30

// digestPromptMaxLines caps the per-event list attached to the chat prompt,
// so the prompt stays within the context of small local models.
const digestPromptMaxLines = 100

// digestLine is one condensed activity in the digest.
type digestLine struct {
	when   time.Time
	camera string
	text   string
}

// digestLines assembles the digest lines from the individual reviews' GenAI
// metadata: one line per distinct activity, ordered by start time.
// Consecutive segments with the same camera and text are collapsed (Frigate
// splits long activities into multiple review segments).
func digestLines(reviews []windowReview) []digestLine {
	var lines []digestLine
	for _, review := range reviews {
		meta := review.Data.Metadata
		if meta == nil {
			continue
		}
		text := strings.TrimSpace(meta.Title)
		if text == "" {
			text = strings.TrimSpace(meta.ShortSummary)
		}
		if text == "" {
			continue
		}
		lines = append(lines, digestLine{
			when:   time.Unix(int64(review.StartTime), 0),
			camera: review.Camera,
			text:   text,
		})
	}
	if len(lines) == 0 {
		return nil
	}
	sort.Slice(lines, func(i, j int) bool {
		if !lines[i].when.Equal(lines[j].when) {
			return lines[i].when.Before(lines[j].when)
		}
		return lines[i].camera < lines[j].camera
	})

	var kept []digestLine
	for i, l := range lines {
		if i > 0 && l.camera == kept[len(kept)-1].camera && l.text == kept[len(kept)-1].text {
			continue
		}
		kept = append(kept, l)
	}
	return kept
}

// digestRender formats the digest lines, at most maxLines (0 = all). The
// cap keeps a plain list message readable; the LLM condensation path passes
// all lines.
func digestRender(lines []digestLine, maxLines int) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, l := range lines {
		if maxLines > 0 && i >= maxLines {
			fmt.Fprintf(&b, "\n…（共 %d 条，已省略后续）", len(lines))
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s %s: %s", l.when.Format("15:04"), l.camera, l.text)
	}
	return b.String()
}

// runDueSchedules fires every schedule that is due at now and has not
// fired today yet, marking each one fired as it is dispatched. Each due
// schedule runs in its own goroutine: a single run can take minutes (a
// GenAI chat doing tool rounds), and running them sequentially in the
// tick loop would push a neighbouring schedule past its exact HH:MM
// minute, silently skipping it for the whole day. Marking at dispatch
// time (not completion) keeps a schedule from double-firing while its
// run is still in flight.
func runDueSchedules(now time.Time, lastFired map[int]string) {
	for i, s := range config.ConfigData.Alerts.General.GenAI.Schedules {
		if !scheduleDue(s, now, lastFired[i]) {
			continue
		}
		lastFired[i] = now.Format("2006-01-02")
		log.Info().
			Int("schedule", i).
			Str("time", s.Time).
			Str("window", s.Window).
			Msg("Scheduled GenAI summary due, running")
		go func(s models.GenAISchedule, i int) {
			if err := RunGenAISchedule(s); err != nil {
				log.Warn().
					Err(err).
					Int("schedule", i).
					Msg("Scheduled GenAI summary failed")
			}
		}(s, i)
	}
}

// StartGenAIScheduler starts the in-process scheduler: a one-minute ticker
// that fires each enabled schedule at most once per day, at its HH:MM in
// container local time. It reads config.ConfigData live on every tick, so a
// config PUT takes effect without a restart.
func StartGenAIScheduler() {
	lastFired := make(map[int]string)
	ticker := time.NewTicker(time.Minute)
	go func() {
		for now := range ticker.C {
			runDueSchedules(now, lastFired)
		}
	}()
}
