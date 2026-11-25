package notifier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
)

// countFileLines returns the number of newline-terminated lines in path.
func countFileLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// enableHistoryFile points persistence at a temp file for the test and
// restores in-memory-only mode afterwards, so other tests are unaffected.
func enableHistoryFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	SetHistoryFile(path)
	t.Cleanup(func() {
		SetHistoryFile("")
		ClearHistory()
	})
	ClearHistory()
	return path
}

func TestHistoryFileRoundTrip(t *testing.T) {
	path := enableHistoryFile(t)
	for i := 0; i < 3; i++ {
		RecordHistory(models.Event{Camera: "cam", Label: "person", ID: fmt.Sprintf("evt-%d", i)}, []string{"webhook"}, false, false)
	}
	if n := countFileLines(t, path); n != 3 {
		t.Fatalf("history file lines = %d, want 3", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if !strings.Contains(lines[0], "evt-0") || !strings.Contains(lines[2], "evt-2") {
		t.Fatalf("file order = %v, want chronological evt-0..evt-2", lines)
	}
	// Simulate a restart: drop the in-memory buffer, reload from disk.
	ClearHistory()
	if len(History()) != 0 {
		t.Fatal("ClearHistory did not empty the buffer")
	}
	SetHistoryFile(path)
	got := History()
	if len(got) != 3 {
		t.Fatalf("History() after reload len = %d, want 3", len(got))
	}
	if got[0].EventID != "evt-2" || got[2].EventID != "evt-0" {
		t.Fatalf("History() after reload = %v, want newest-first evt-2..evt-0", got)
	}
	if got[0].Camera != "cam" || got[0].Providers[0] != "webhook" {
		t.Errorf("reloaded entry = %+v, want camera cam + webhook provider", got[0])
	}
}

func TestHistoryFileLoadCapsAt100(t *testing.T) {
	path := enableHistoryFile(t)
	for i := 0; i < 105; i++ {
		RecordHistory(models.Event{Camera: "cam", Label: "person", ID: fmt.Sprintf("evt-%d", i)}, []string{"webhook"}, false, false)
	}
	if n := countFileLines(t, path); n != 105 {
		t.Fatalf("history file lines = %d, want 105", n)
	}
	ClearHistory()
	SetHistoryFile(path)
	got := History()
	if len(got) != maxHistoryEntries {
		t.Fatalf("History() after reload len = %d, want %d", len(got), maxHistoryEntries)
	}
	if got[0].EventID != "evt-104" || got[len(got)-1].EventID != "evt-5" {
		t.Fatalf("History() after reload spans %s..%s, want evt-104..evt-5", got[0].EventID, got[len(got)-1].EventID)
	}
}

func TestHistoryFileSkipsCorruptLines(t *testing.T) {
	path := enableHistoryFile(t)
	RecordHistory(models.Event{Camera: "cam", Label: "person", ID: "evt-0"}, []string{"webhook"}, false, false)
	RecordHistory(models.Event{Camera: "cam", Label: "person", ID: "evt-1"}, []string{"webhook"}, false, false)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	fmt.Fprintln(f, "this is not json")
	fmt.Fprintln(f, `{"time":`)
	fmt.Fprintln(f)
	f.Close()
	ClearHistory()
	SetHistoryFile(path)
	got := History()
	if len(got) != 2 {
		t.Fatalf("History() after reload len = %d, want 2 (corrupt lines skipped)", len(got))
	}
	if got[0].EventID != "evt-1" || got[1].EventID != "evt-0" {
		t.Fatalf("History() after reload = %v, want evt-1, evt-0", got)
	}
}

func TestHistoryFileCompactsPastBound(t *testing.T) {
	path := enableHistoryFile(t)
	for i := 0; i < 1005; i++ {
		RecordHistory(models.Event{Camera: "cam", Label: "person", ID: fmt.Sprintf("evt-%d", i)}, []string{"webhook"}, false, false)
	}
	// Past the 1000-line bound the file is rewritten from the in-memory
	// buffer (100 entries), so it must be far shorter than 1005 lines.
	n := countFileLines(t, path)
	if n > 200 {
		t.Fatalf("history file lines = %d, want <= 200 after compaction", n)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("compaction temp file left behind: %v", err)
	}
	got := History()
	if len(got) != maxHistoryEntries {
		t.Fatalf("History() len = %d, want %d", len(got), maxHistoryEntries)
	}
	if got[0].EventID != "evt-1004" {
		t.Fatalf("newest entry = %s, want evt-1004", got[0].EventID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "evt-1004") {
		t.Fatal("compacted file does not contain the newest entry")
	}
}
