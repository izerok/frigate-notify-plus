package notifier

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/models"
)

// maxHistoryEntries caps the in-memory notification history shown in the Web UI.
const maxHistoryEntries = 100

// maxHistoryFileLines bounds the on-disk history file. Past it the file is
// compacted back to the entries currently held in memory (at most
// maxHistoryEntries lines), so the file grows only between compactions.
const maxHistoryFileLines = 1000

// HistoryEntry is one dispatched notification as shown on the Web UI history page.
type HistoryEntry struct {
	Time          time.Time `json:"time"`
	EventID       string    `json:"event_id,omitempty"`
	ReviewID      string    `json:"review_id,omitempty"`
	Camera        string    `json:"camera"`
	Labels        []string  `json:"labels,omitempty"`
	GenAITitle    string    `json:"genai_title,omitempty"`
	GenAISummary  string    `json:"genai_summary,omitempty"`
	GenAIThreat   string    `json:"genai_threat,omitempty"`
	Description   string    `json:"description,omitempty"`
	Providers     []string  `json:"providers,omitempty"`
	IsGenAIUpdate bool      `json:"is_genai_update,omitempty"`
	IsTest        bool      `json:"is_test,omitempty"`
	OK            bool      `json:"ok"`
	Detail        string    `json:"detail,omitempty"`
}

var (
	historyMu      sync.Mutex
	historyEntries []HistoryEntry

	// historyFile is the JSON Lines file entries are appended to, one per
	// line, when persistence is enabled (SetHistoryFile). Empty means
	// in-memory only, as before. historyFileLines counts the lines
	// currently in the file, for the compaction bound.
	historyFile      string
	historyFileLines int
)

// SetHistoryFile enables on-disk persistence: every recorded entry is
// appended to path as one JSON line, and any existing file is loaded into
// the in-memory buffer (newest 100). A missing file is normal on first run;
// corrupt lines are skipped and never fail startup. Pass "" to disable
// persistence again. Loading replaces whatever the in-memory buffer held,
// so call this before anything records history (main does it at startup).
func SetHistoryFile(path string) {
	historyMu.Lock()
	defer historyMu.Unlock()
	historyFile = path
	historyFileLines = 0
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Warn().Err(err).Msgf("Cannot create history directory for %q, history stays in memory only", path)
		historyFile = ""
		return
	}
	loadHistoryFileLocked()
}

// loadHistoryFileLocked reads the history file into the in-memory buffer,
// newest first. Callers must hold historyMu and have set historyFile.
func loadHistoryFileLocked() {
	f, err := os.Open(historyFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Msgf("Cannot open history file %q, starting with empty history", historyFile)
		}
		return
	}
	defer f.Close()

	// The file is chronological (oldest first); collect the valid lines,
	// then keep the newest maxHistoryEntries.
	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		historyFileLines++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // corrupt line: skip, never fail startup
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Msgf("Error reading history file %q, keeping what was loaded", historyFile)
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	historyEntries = nil
	for i := len(entries) - 1; i >= 0; i-- {
		historyEntries = append(historyEntries, entries[i])
	}
	if len(historyEntries) > 0 {
		log.Info().Msgf("Loaded %d history entries from %q", len(historyEntries), historyFile)
	}
}

// appendHistoryLocked appends one entry to the history file, then compacts
// the file when it passes the line bound. Callers must hold historyMu.
// A failed append only warns: persistence must never break a notification.
func appendHistoryLocked(entry HistoryEntry) {
	if historyFile == "" {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Warn().Err(err).Msg("Cannot encode history entry, skipping disk append")
		return
	}
	f, err := os.OpenFile(historyFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Warn().Err(err).Msgf("Cannot append to history file %q", historyFile)
		return
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Warn().Err(err).Msgf("Cannot write history file %q", historyFile)
		f.Close()
		return
	}
	f.Close()
	historyFileLines++
	if historyFileLines > maxHistoryFileLines {
		compactHistoryFileLocked()
	}
}

// compactHistoryFileLocked rewrites the history file from the in-memory
// buffer (oldest first), via a temp file + rename so a crash mid-rewrite
// never loses the whole file. Callers must hold historyMu.
func compactHistoryFileLocked() {
	tmp := historyFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		log.Warn().Err(err).Msgf("Cannot compact history file %q", historyFile)
		return
	}
	lines := 0
	ok := true
	for i := len(historyEntries) - 1; i >= 0; i-- {
		line, err := json.Marshal(historyEntries[i])
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			log.Warn().Err(err).Msgf("Cannot write compacted history file %q", tmp)
			ok = false
			break
		}
		lines++
	}
	if err := f.Close(); err != nil {
		log.Warn().Err(err).Msgf("Cannot close compacted history file %q", tmp)
		ok = false
	}
	if !ok {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, historyFile); err != nil {
		log.Warn().Err(err).Msgf("Cannot replace history file %q", historyFile)
		os.Remove(tmp)
		return
	}
	historyFileLines = lines
}

// prependHistory adds one entry at the front, newest first, trimming to the
// ring-buffer cap, and appends it to the history file when persistence is on.
func prependHistory(entry HistoryEntry) {
	historyMu.Lock()
	defer historyMu.Unlock()
	historyEntries = append([]HistoryEntry{entry}, historyEntries...)
	if len(historyEntries) > maxHistoryEntries {
		historyEntries = historyEntries[:maxHistoryEntries]
	}
	appendHistoryLocked(entry)
}

// labelsOf collects the label/sublabel chips for an entry.
func labelsOf(event models.Event) []string {
	labels := []string{}
	if event.Label != "" {
		labels = append(labels, event.Label)
	}
	if event.SubLabel != "" {
		labels = append(labels, event.SubLabel)
	}
	return labels
}

// RecordHistory prepends one entry for a dispatched alert. GenAI updates are
// recorded as their own entry so the history shows the initial alert and the
// updated text as two rows. With a data directory set, the entry is also
// appended to $DATA_DIR/history.jsonl and survives restarts; without one,
// history lives only in memory and clears on restart, as before.
func RecordHistory(event models.Event, providers []string, isGenAIUpdate bool, isTest bool) {
	detail := strings.Join(providers, ", ")
	if isGenAIUpdate {
		detail += " · GenAI update"
	}
	if isTest {
		detail += " · test"
	}
	entry := HistoryEntry{
		Time:          time.Now(),
		EventID:       event.ID,
		ReviewID:      event.Extra.ReviewID,
		Camera:        event.Camera,
		Labels:        labelsOf(event),
		GenAITitle:    event.Extra.GenAITitle,
		GenAISummary:  event.Extra.GenAISummary,
		GenAIThreat:   event.Extra.GenAIThreatLevel,
		Description:   event.Extra.Description,
		Providers:     providers,
		IsGenAIUpdate: isGenAIUpdate,
		IsTest:        isTest,
		OK:            len(providers) > 0,
		Detail:        detail,
	}
	prependHistory(entry)
}

// RecordMutedDrop prepends one entry for an alert dropped because its
// camera is muted. The entry is a failure (OK=false) whose detail names the
// time the mute lifts; no providers are listed.
func RecordMutedDrop(event models.Event, until time.Time) {
	entry := HistoryEntry{
		Time:          time.Now(),
		EventID:       event.ID,
		ReviewID:      event.Extra.ReviewID,
		Camera:        event.Camera,
		Labels:        labelsOf(event),
		GenAITitle:    event.Extra.GenAITitle,
		GenAISummary:  event.Extra.GenAISummary,
		GenAIThreat:   event.Extra.GenAIThreatLevel,
		Description:   event.Extra.Description,
		Providers:     nil,
		IsGenAIUpdate: event.Extra.IsGenAIUpdate,
		IsTest:        false,
		OK:            false,
		Detail:        "已静音至 " + until.Format("15:04"),
	}
	prependHistory(entry)
}

// RecordCapDrop prepends one entry for an alert dropped by the hourly
// notification cap. The entry is a failure (OK=false); no providers are listed.
func RecordCapDrop(event models.Event) {
	entry := HistoryEntry{
		Time:          time.Now(),
		EventID:       event.ID,
		ReviewID:      event.Extra.ReviewID,
		Camera:        event.Camera,
		Labels:        labelsOf(event),
		GenAITitle:    event.Extra.GenAITitle,
		GenAISummary:  event.Extra.GenAISummary,
		GenAIThreat:   event.Extra.GenAIThreatLevel,
		Description:   event.Extra.Description,
		Providers:     nil,
		IsGenAIUpdate: event.Extra.IsGenAIUpdate,
		IsTest:        false,
		OK:            false,
		Detail:        "已达每小时上限",
	}
	prependHistory(entry)
}

// RecordEscalationHistory prepends one entry for a GenAI escalation alert: a
// fresh message sent to escalation targets when a GenAI update reaches a
// rule's MinThreat. With no providers the entry is a failure (OK=false),
// e.g. when the escalation was dropped by the hourly cap.
func RecordEscalationHistory(event models.Event, providers []string) {
	detail := strings.Join(providers, ", ")
	if detail != "" {
		detail += " · "
	}
	detail += "GenAI 升级提醒"
	entry := HistoryEntry{
		Time:          time.Now(),
		EventID:       event.ID,
		ReviewID:      event.Extra.ReviewID,
		Camera:        event.Camera,
		Labels:        labelsOf(event),
		GenAITitle:    event.Extra.GenAITitle,
		GenAISummary:  event.Extra.GenAISummary,
		GenAIThreat:   event.Extra.GenAIThreatLevel,
		Description:   event.Extra.Description,
		Providers:     providers,
		IsGenAIUpdate: false,
		IsTest:        false,
		OK:            len(providers) > 0,
		Detail:        detail,
	}
	prependHistory(entry)
}

// RecordScheduledSummary prepends one entry for a GenAI summary (scheduled
// or idle-triggered). The summary covers all cameras, so Camera reads
// "全部相机". The label names the kind, e.g. "定时总结 今天" or "活动总结".
// With no providers the entry is a failure (OK=false), e.g. when the summary
// was dropped by the hourly cap (the caller appends the reason to label).
func RecordScheduledSummary(event models.Event, providers []string, label string) {
	detail := strings.Join(providers, ", ")
	if detail != "" {
		detail += " · "
	}
	detail += label
	entry := HistoryEntry{
		Time:          time.Now(),
		EventID:       event.ID,
		ReviewID:      event.Extra.ReviewID,
		Camera:        "全部相机",
		Labels:        labelsOf(event),
		GenAITitle:    event.Extra.GenAITitle,
		GenAISummary:  event.Extra.GenAISummary,
		GenAIThreat:   event.Extra.GenAIThreatLevel,
		Description:   event.Extra.Description,
		Providers:     providers,
		IsGenAIUpdate: false,
		IsTest:        false,
		OK:            len(providers) > 0,
		Detail:        detail,
	}
	prependHistory(entry)
}

// History returns a copy of the recorded entries, newest first.
func History() []HistoryEntry {
	historyMu.Lock()
	defer historyMu.Unlock()
	out := make([]HistoryEntry, len(historyEntries))
	copy(out, historyEntries)
	return out
}

// ClearHistory empties the recorded entries. Used by tests.
func ClearHistory() {
	historyMu.Lock()
	defer historyMu.Unlock()
	historyEntries = nil
}
