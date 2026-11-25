package notifier

import (
	"sync"
	"time"
)

// hourlyCapTracker counts dispatched alerts in a rolling 1-hour window, for
// the global hourly notification cap (alerts.general.max_notifications_per_hour).
// Only DISPATCHED alerts count: filtered, muted and cooled-down alerts never
// reach the gate, and test alerts are exempt. History is in-memory and resets
// when the application restarts.
type hourlyCapTracker struct {
	mu     sync.Mutex
	stamps []time.Time
	now    func() time.Time
}

var hourlyCap = hourlyCapTracker{now: time.Now}

// allow reports whether one more dispatch fits under cap, and reserves the
// slot when it does. A cap <= 0 means unlimited.
func (t *hourlyCapTracker) allow(cap int) bool {
	if cap <= 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.allowAt(cap, t.now())
}

// allowAt is the testable core: prune stamps older than one hour, then
// admit when fewer than cap dispatches remain in the window. The caller
// must hold mu.
func (t *hourlyCapTracker) allowAt(cap int, now time.Time) bool {
	cutoff := now.Add(-time.Hour)
	kept := t.stamps[:0]
	for _, s := range t.stamps {
		if s.After(cutoff) {
			kept = append(kept, s)
		}
	}
	t.stamps = kept
	if len(t.stamps) >= cap {
		return false
	}
	t.stamps = append(t.stamps, now)
	return true
}

// reset clears the recorded dispatches. Used by tests.
func (t *hourlyCapTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stamps = nil
}
