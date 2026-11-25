package notifier

import (
	"slices"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// threatLevel maps an event's GenAI threat level to a number: Normal 0,
// Suspicious 1, Critical 2. Events without GenAI data (or with an
// unrecognised level) are 0.
func threatLevel(event models.Event) int {
	switch event.Extra.GenAIThreatLevel {
	case "Critical":
		return 2
	case "Suspicious":
		return 1
	default:
		return 0
	}
}

// hasGenAI reports whether an event carries GenAI data.
func hasGenAI(event models.Event) bool {
	return event.Extra.GenAIThreatLevel != ""
}

// ruleMatches reports whether a routing rule matches a set of events. Every
// non-empty condition list must match (OR within a list); an empty list
// matches everything. MinThreat -1 always passes, otherwise the highest
// threat level across the events must reach MinThreat.
func ruleMatches(rule models.RoutingRule, events []models.Event) bool {
	if len(rule.Cameras) > 0 {
		matched := false
		for _, event := range events {
			if slices.Contains(rule.Cameras, event.Camera) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(rule.Labels) > 0 {
		matched := false
		for _, event := range events {
			if slices.Contains(rule.Labels, event.Label) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(rule.Zones) > 0 {
		matched := false
		for _, event := range events {
			for _, zone := range append(slices.Clone(event.CurrentZones), event.Zones...) {
				if slices.Contains(rule.Zones, zone) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(rule.Sublabels) > 0 {
		matched := false
		for _, event := range events {
			if slices.Contains(rule.Sublabels, event.SubLabel) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if minThreat := models.DerefInt(rule.MinThreat, -1); minThreat != -1 {
		maxThreat := 0
		for _, event := range events {
			if level := threatLevel(event); level > maxThreat {
				maxThreat = level
			}
		}
		if maxThreat < minThreat {
			return false
		}
	}
	return true
}

// applyRouting narrows the eligible senders to the targets of the matching
// routing rules. With no rules configured, or when no rule matches, the
// senders are returned unchanged (today's behavior: every eligible provider
// receives the alert). A target with index -1 matches every profile of that
// provider.
func applyRouting(senders []alertSender, events []models.Event) []alertSender {
	rules := config.ConfigData.Alerts.Routing.Rules
	if len(rules) == 0 {
		return senders
	}
	targets := make(map[notifMeta]bool)
	matched := false
	for _, rule := range rules {
		if !ruleMatches(rule, events) {
			continue
		}
		matched = true
		for _, target := range rule.Targets {
			targets[notifMeta{name: target.Provider, index: models.DerefInt(target.Index, -1)}] = true
		}
	}
	if !matched {
		return senders
	}
	var kept []alertSender
	for _, sender := range senders {
		if targets[notifMeta{name: sender.name, index: sender.index}] ||
			targets[notifMeta{name: sender.name, index: -1}] {
			kept = append(kept, sender)
		}
	}
	return kept
}
