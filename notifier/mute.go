package notifier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
)

// MuteEntry is one per-camera mute as returned by the mute API.
type MuteEntry struct {
	Camera           string    `json:"camera"`
	Until            time.Time `json:"until"`
	RemainingSeconds int64     `json:"remaining_seconds"`
}

var (
	muteMu sync.RWMutex
	mutes  map[string]time.Time
)

// muteFilePath is mute.json next to the config file.
func muteFilePath() string {
	return filepath.Join(filepath.Dir(config.ConfigFile), "mute.json")
}

// MuteCamera mutes a camera for the given number of minutes (1..4320) and
// returns when the mute expires. The mute is persisted best-effort.
func MuteCamera(camera string, minutes int) (time.Time, error) {
	if camera == "" {
		return time.Time{}, fmt.Errorf("camera must not be empty")
	}
	if minutes < 1 || minutes > 4320 {
		return time.Time{}, fmt.Errorf("minutes must be between 1 and 4320")
	}
	until := time.Now().Add(time.Duration(minutes) * time.Minute)
	muteMu.Lock()
	if mutes == nil {
		mutes = make(map[string]time.Time)
	}
	mutes[camera] = until
	muteMu.Unlock()
	saveMutes()
	return until, nil
}

// UnmuteCamera removes a camera's mute, reporting whether one existed.
func UnmuteCamera(camera string) bool {
	muteMu.Lock()
	_, ok := mutes[camera]
	if ok {
		delete(mutes, camera)
	}
	muteMu.Unlock()
	if ok {
		saveMutes()
	}
	return ok
}

// ListMutes returns the active mutes, pruned of expired entries and sorted
// by camera name.
func ListMutes() []MuteEntry {
	now := time.Now()
	muteMu.Lock()
	pruned := false
	out := make([]MuteEntry, 0)
	for camera, until := range mutes {
		if !until.After(now) {
			delete(mutes, camera)
			pruned = true
			continue
		}
		out = append(out, MuteEntry{
			Camera:           camera,
			Until:            until,
			RemainingSeconds: int64(time.Until(until).Seconds()),
		})
	}
	muteMu.Unlock()
	if pruned {
		saveMutes()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Camera < out[j].Camera })
	return out
}

// IsMuted reports whether a camera is currently muted, and until when.
func IsMuted(camera string) (bool, time.Time) {
	muteMu.Lock()
	defer muteMu.Unlock()
	until, ok := mutes[camera]
	if !ok {
		return false, time.Time{}
	}
	if !until.After(time.Now()) {
		delete(mutes, camera)
		return false, time.Time{}
	}
	return true, until
}

// LoadMutes reads mute.json next to the config file. A missing or corrupt
// file is not fatal: a warning is logged and muting starts empty.
func LoadMutes() {
	data, err := os.ReadFile(muteFilePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Msg("Unable to read mute file, starting with no mutes")
		}
		muteMu.Lock()
		mutes = make(map[string]time.Time)
		muteMu.Unlock()
		return
	}
	var loaded map[string]time.Time
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Warn().Err(err).Msg("Unable to parse mute file, starting with no mutes")
		muteMu.Lock()
		mutes = make(map[string]time.Time)
		muteMu.Unlock()
		return
	}
	if loaded == nil {
		loaded = make(map[string]time.Time)
	}
	now := time.Now()
	for camera, until := range loaded {
		if !until.After(now) {
			delete(loaded, camera)
		}
	}
	muteMu.Lock()
	mutes = loaded
	muteMu.Unlock()
}

// saveMutes persists the current mutes best-effort; failures only warn.
func saveMutes() {
	muteMu.RLock()
	snapshot := make(map[string]time.Time, len(mutes))
	for camera, until := range mutes {
		snapshot[camera] = until
	}
	muteMu.RUnlock()
	data, err := json.Marshal(snapshot)
	if err != nil {
		log.Warn().Err(err).Msg("Unable to encode mutes")
		return
	}
	if err := os.WriteFile(muteFilePath(), data, 0644); err != nil {
		log.Warn().Err(err).Msg("Unable to save mutes")
	}
}
