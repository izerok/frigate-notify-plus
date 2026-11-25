package notifier

import (
	"slices"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// escalatedRules tracks (review ID, escalation rule index) pairs that already
// fired, so the same rule escalates a review at most once. A later GenAI
// update with a still-higher threat can still fire a HIGHER rule, because
// each rule is tracked separately. The map is claimed before the dispatch
// starts, so a rule is never double-fired by two updates racing each other;
// a rule claimed but then dropped (mute, hourly cap, no eligible target)
// stays claimed for that review. In-memory only: a restart may re-fire a
// rule for a review that already escalated.
var (
	escalatedMu    sync.Mutex
	escalatedRules = make(map[string]map[int]bool)
)

// matchingEscalationRules returns the indexes of escalation rules whose
// MinThreat the events' highest threat level reaches and that have not fired
// for this review yet, marking them fired as it goes.
func matchingEscalationRules(reviewID string, events []models.Event) []int {
	rules := config.ConfigData.Alerts.Routing.Escalation
	if len(rules) == 0 {
		return nil
	}
	maxThreat := 0
	for _, event := range events {
		if level := threatLevel(event); level > maxThreat {
			maxThreat = level
		}
	}
	escalatedMu.Lock()
	defer escalatedMu.Unlock()
	fired, ok := escalatedRules[reviewID]
	if !ok {
		fired = make(map[int]bool)
		escalatedRules[reviewID] = fired
	}
	var matched []int
	for i, rule := range rules {
		if fired[i] || maxThreat < models.DerefInt(rule.MinThreat, 1) {
			continue
		}
		fired[i] = true
		matched = append(matched, i)
	}
	return matched
}

// clearEscalatedRules forgets every fired (review, rule) pair. Used by tests.
func clearEscalatedRules() {
	escalatedMu.Lock()
	defer escalatedMu.Unlock()
	escalatedRules = make(map[string]map[int]bool)
}

// escalateIfNeeded sends a fresh alert to the targets of newly-matching
// escalation rules. It runs at the end of sendAlert for a GenAI update, after
// the caption edit on the originally notified channels has been dispatched.
// The fresh alert bypasses the cooldown gate (like GenAI updates do) but
// still counts toward the hourly cap, and per-profile filters still apply on
// top of the escalation targets.
func escalateIfNeeded(events []models.Event) {
	if len(events) == 0 || events[0].Extra.ReviewID == "" {
		return
	}
	rules := config.ConfigData.Alerts.Routing.Escalation
	if len(rules) == 0 {
		return
	}
	matched := matchingEscalationRules(events[0].Extra.ReviewID, events)
	if len(matched) == 0 {
		return
	}
	// Union of the newly-matching rules' targets.
	targets := make(map[notifMeta]bool)
	for _, i := range matched {
		for _, target := range rules[i].Targets {
			targets[notifMeta{name: target.Provider, index: models.DerefInt(target.Index, -1)}] = true
		}
	}
	// Fresh alert: clear the GenAI-update flag on a copy so providers send a
	// NEW message instead of editing the one the initial alert sent.
	fresh := slices.Clone(events)
	for i := range fresh {
		fresh[i].Extra.IsGenAIUpdate = false
	}
	match := func(s alertSender) bool {
		return targets[notifMeta{name: s.name, index: s.index}] ||
			targets[notifMeta{name: s.name, index: -1}]
	}
	log.Info().
		Str("review_id", events[0].Extra.ReviewID).
		Ints("rules", matched).
		Msg("GenAI escalation: sending fresh alert to escalation targets")
	sendAlert(fresh, sendOptions{
		bypassCooldown: true,
		match:          match,
		isEscalation:   true,
		skipEscalation: true,
	})
}
