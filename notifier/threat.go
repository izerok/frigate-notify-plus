package notifier

import (
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// threatPriorityFor returns the configured per-threat-level priority for a
// provider ("ntfy", "gotify", "pushover") at the given threat level
// (0 Normal, 1 Suspicious, 2 Critical). The second return value is false
// for providers without a threat-priority mapping.
func threatPriorityFor(provider string, level int) (int, bool) {
	tp := config.ConfigData.Alerts.General.ThreatPriority
	switch provider {
	case "ntfy":
		switch level {
		case 2:
			return tp.NtfyCritical, true
		case 1:
			return tp.NtfySuspicious, true
		default:
			return tp.NtfyNormal, true
		}
	case "gotify":
		switch level {
		case 2:
			return tp.GotifyCritical, true
		case 1:
			return tp.GotifySuspicious, true
		default:
			return tp.GotifyNormal, true
		}
	case "pushover":
		switch level {
		case 2:
			return tp.PushoverCritical, true
		case 1:
			return tp.PushoverSuspicious, true
		default:
			return tp.PushoverNormal, true
		}
	default:
		return 0, false
	}
}

// telegramSilentFor reports whether a Telegram alert for the given threat
// level should be sent without sound (disable_notification).
func telegramSilentFor(level int) bool {
	tp := config.ConfigData.Alerts.General.ThreatPriority
	switch level {
	case 2:
		return tp.TelegramSilentCritical
	case 1:
		return tp.TelegramSilentSuspicious
	default:
		return tp.TelegramSilentNormal
	}
}

// telegramSilent reports whether a Telegram alert for this event should be
// sent without sound. Silence only ever applies to events carrying GenAI
// data; without GenAI data the alert is always audible.
func telegramSilent(event models.Event) bool {
	return hasGenAI(event) && telegramSilentFor(threatLevel(event))
}

// gotifyPriority resolves the Gotify priority for an event: the profile's
// own priority wins when set (non-zero), otherwise the threat-level mapping.
func gotifyPriority(profile models.Gotify, event models.Event) int {
	if profile.Priority != 0 {
		return profile.Priority
	}
	if mapped, ok := threatPriorityFor("gotify", threatLevel(event)); ok {
		return mapped
	}
	return 0
}

// pushoverPriority resolves the Pushover priority for an event: the
// profile's own priority wins when set (non-zero), otherwise the
// threat-level mapping.
func pushoverPriority(profile models.Pushover, event models.Event) int {
	if profile.Priority != 0 {
		return profile.Priority
	}
	if mapped, ok := threatPriorityFor("pushover", threatLevel(event)); ok {
		return mapped
	}
	return 0
}
