package notifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/izerok/frigate-notify-plus/config"
)

// setupMuteDir points config.ConfigFile at a temp dir so mute.json lives
// there, and restores the previous value and mute state on cleanup.
func setupMuteDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origFile := config.ConfigFile
	origMutes := mutes
	config.ConfigFile = filepath.Join(dir, "config.yml")
	mutes = nil
	t.Cleanup(func() {
		config.ConfigFile = origFile
		mutes = origMutes
	})
	return dir
}

func TestMuteCameraValidation(t *testing.T) {
	setupMuteDir(t)
	if _, err := MuteCamera("", 30); err == nil {
		t.Error("expected an error for an empty camera")
	}
	for _, minutes := range []int{0, -5, 4321} {
		if _, err := MuteCamera("front_door", minutes); err == nil {
			t.Errorf("expected an error for minutes=%d", minutes)
		}
	}
	if _, err := MuteCamera("front_door", 4320); err != nil {
		t.Errorf("minutes=4320 should be accepted, got %v", err)
	}
}

func TestMuteSetListUnmute(t *testing.T) {
	setupMuteDir(t)

	if _, err := MuteCamera("front_door", 30); err != nil {
		t.Fatalf("MuteCamera failed: %v", err)
	}
	if _, err := MuteCamera("garage", 60); err != nil {
		t.Fatalf("MuteCamera failed: %v", err)
	}

	muted, until := IsMuted("front_door")
	if !muted {
		t.Fatal("front_door should be muted")
	}
	if time.Until(until) <= 0 || time.Until(until) > 31*time.Minute {
		t.Errorf("front_door mute should expire in ~30m, got %v", time.Until(until))
	}
	if muted, _ := IsMuted("driveway"); muted {
		t.Error("driveway should not be muted")
	}

	list := ListMutes()
	if len(list) != 2 {
		t.Fatalf("ListMutes = %d entries, want 2", len(list))
	}
	if list[0].Camera != "front_door" || list[1].Camera != "garage" {
		t.Errorf("ListMutes not sorted by camera: %+v", list)
	}
	for _, entry := range list {
		if entry.RemainingSeconds <= 0 {
			t.Errorf("entry %+v should have a positive remaining_seconds", entry)
		}
		if entry.Until.IsZero() {
			t.Errorf("entry %+v should have an until time", entry)
		}
	}

	if !UnmuteCamera("front_door") {
		t.Error("UnmuteCamera should report an existing mute")
	}
	if muted, _ := IsMuted("front_door"); muted {
		t.Error("front_door should no longer be muted")
	}
	if UnmuteCamera("front_door") {
		t.Error("UnmuteCamera should report no mute the second time")
	}
	if list := ListMutes(); len(list) != 1 || list[0].Camera != "garage" {
		t.Errorf("ListMutes after unmute = %+v, want only garage", list)
	}
}

func TestMutePrunesExpired(t *testing.T) {
	setupMuteDir(t)

	muteMu.Lock()
	mutes = map[string]time.Time{
		"front_door": time.Now().Add(30 * time.Minute),
		"old_cam":    time.Now().Add(-1 * time.Minute),
	}
	muteMu.Unlock()

	if muted, _ := IsMuted("old_cam"); muted {
		t.Error("expired mute should not report as muted")
	}
	list := ListMutes()
	if len(list) != 1 || list[0].Camera != "front_door" {
		t.Errorf("ListMutes = %+v, want only front_door", list)
	}
}

func TestMutePersistRoundTrip(t *testing.T) {
	dir := setupMuteDir(t)

	if _, err := MuteCamera("front_door", 30); err != nil {
		t.Fatalf("MuteCamera failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mute.json")); err != nil {
		t.Fatalf("mute.json was not written: %v", err)
	}

	// Drop the in-memory state and reload from disk.
	muteMu.Lock()
	mutes = nil
	muteMu.Unlock()
	LoadMutes()

	if muted, _ := IsMuted("front_door"); !muted {
		t.Error("front_door mute should survive a reload from mute.json")
	}
}

func TestLoadMutesMissingAndCorrupt(t *testing.T) {
	dir := setupMuteDir(t)

	// Missing file: not fatal, starts empty.
	LoadMutes()
	if list := ListMutes(); len(list) != 0 {
		t.Errorf("ListMutes with no file = %+v, want empty", list)
	}

	// Corrupt file: not fatal, starts empty.
	if err := os.WriteFile(filepath.Join(dir, "mute.json"), []byte("{not json"), 0644); err != nil {
		t.Fatalf("failed to write corrupt mute file: %v", err)
	}
	LoadMutes()
	if list := ListMutes(); len(list) != 0 {
		t.Errorf("ListMutes with corrupt file = %+v, want empty", list)
	}
}

func TestSendAlertMutedDrop(t *testing.T) {
	hits := setupTwoWebhooks(t)
	setupMuteDir(t)
	ClearHistory()
	t.Cleanup(ClearHistory)

	if _, err := MuteCamera("front_door", 30); err != nil {
		t.Fatalf("MuteCamera failed: %v", err)
	}

	sentinel := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	config.Internal.Status.LastNotification = sentinel

	// A real (non-test) alert for a muted camera is dropped: no webhook
	// delivery, no cooldown stamp, and a muted-drop history entry.
	SendAlert(testAlertEvents)

	time.Sleep(300 * time.Millisecond)
	if got := hits[0].Load(); got != 0 {
		t.Errorf("profile 0 hits = %d, want 0 (muted)", got)
	}
	if got := hits[1].Load(); got != 0 {
		t.Errorf("profile 1 hits = %d, want 0 (muted)", got)
	}
	if !config.Internal.Status.LastNotification.Equal(sentinel) {
		t.Error("muted drop must not stamp LastNotification")
	}
	history := History()
	if len(history) != 1 {
		t.Fatalf("history has %d entries, want 1 muted drop", len(history))
	}
	entry := history[0]
	if entry.OK {
		t.Error("muted-drop history entry should have ok=false")
	}
	if entry.Camera != "front_door" {
		t.Errorf("muted-drop camera = %q, want front_door", entry.Camera)
	}
	if !strings.HasPrefix(entry.Detail, "已静音至") {
		t.Errorf("muted-drop detail = %q, want it to start with 已静音至", entry.Detail)
	}
	if len(entry.Providers) != 0 {
		t.Errorf("muted-drop providers = %v, want none", entry.Providers)
	}

	// A test alert bypasses the mute.
	SendTestAlert(testAlertEvents)
	waitForHits(t, hits[0], 1)
	waitForHits(t, hits[1], 1)
}
