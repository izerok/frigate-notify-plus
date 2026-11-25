package notifier

import (
	"testing"
	"time"
)

func TestHourlyCapTracker(t *testing.T) {
	tracker := &hourlyCapTracker{now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	now := tracker.now()

	// Unlimited when the cap is 0 (or negative).
	for i := 0; i < 5; i++ {
		if !tracker.allow(0) {
			t.Fatalf("allow(0) = false on call %d, want true (unlimited)", i)
		}
	}
	if !tracker.allow(-1) {
		t.Fatal("allow(-1) = false, want true (unlimited)")
	}

	// Cap 2: two dispatches fit, the third does not.
	tracker.reset()
	if !tracker.allowAt(2, now) || !tracker.allowAt(2, now.Add(30*time.Minute)) {
		t.Fatal("first two dispatches should fit under cap 2")
	}
	if tracker.allowAt(2, now.Add(31*time.Minute)) {
		t.Error("third dispatch inside the hour should be capped")
	}

	// The window slides: 60 minutes after the first dispatch, its slot frees
	// (a stamp exactly one hour old is pruned), so a new dispatch fits...
	if !tracker.allowAt(2, now.Add(60*time.Minute)) {
		t.Error("dispatch 60 minutes after the first should fit again")
	}
	// ... but the second dispatch (30 minutes in) still occupies a slot at
	// +61 minutes, so the cap is hit again...
	if tracker.allowAt(2, now.Add(61*time.Minute)) {
		t.Error("dispatch at +61 minutes should be capped (second slot still counts)")
	}
	// ... and once the second slot ages out too, the cap frees again.
	if !tracker.allowAt(2, now.Add(91*time.Minute)) {
		t.Error("dispatch 91 minutes after the first should fit again")
	}

	// A stamp exactly one hour old is pruned (After, not on-or-after), so a
	// dispatch exactly one hour later fits again; just before the hour the
	// slot still counts.
	tracker.reset()
	if !tracker.allowAt(1, now) {
		t.Fatal("first dispatch should fit under cap 1")
	}
	if tracker.allowAt(1, now.Add(59*time.Minute)) {
		t.Error("dispatch 59 minutes later should still be capped")
	}
	if !tracker.allowAt(1, now.Add(time.Hour)) {
		t.Error("dispatch exactly one hour later should fit again (old stamp pruned)")
	}
}
