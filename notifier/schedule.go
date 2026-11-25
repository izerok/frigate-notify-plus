package notifier

import (
	"fmt"
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

// RunGenAISchedule executes one scheduled summary now: it asks Frigate to
// summarize the schedule's window and sends the text to the schedule's
// targets (all enabled channels when targets is empty). Unlike the idle
// activity summary, a scheduled digest reports even a quiet period — no
// concerns gate — because Frigate returns a placeholder line for a window
// with nothing suspicious, which is itself the digest. Returns an error when
// GenAI is off, Frigate fails, or the summary is empty.
func RunGenAISchedule(s models.GenAISchedule) error {
	if !config.ConfigData.Alerts.General.GenAI.Enabled {
		return fmt.Errorf("GenAI is not enabled")
	}
	now := time.Now()
	start, end := scheduleWindow(s, now)
	summary, err := requestFrigateSummary(start, end)
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

// StartGenAIScheduler starts the in-process scheduler: a one-minute ticker
// that fires each enabled schedule at most once per day, at its HH:MM in
// container local time. It reads config.ConfigData live on every tick, so a
// config PUT takes effect without a restart.
func StartGenAIScheduler() {
	lastFired := make(map[int]string)
	ticker := time.NewTicker(time.Minute)
	go func() {
		for now := range ticker.C {
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
				if err := RunGenAISchedule(s); err != nil {
					log.Warn().
						Err(err).
						Int("schedule", i).
						Msg("Scheduled GenAI summary failed")
				}
			}
		}
	}()
}
