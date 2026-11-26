package notifier

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util"
)

var (
	summaryMu       sync.Mutex
	summaryTimer    *time.Timer
	flurryStartTime time.Time
	flurryActive    bool
)

type summaryResponse struct {
	Success bool   `json:"success"`
	Summary string `json:"summary"`
	Message string `json:"message"`
	// FastAPI auth/permission errors (e.g. 403 without full camera access) use
	// "detail" instead of "message".
	Detail string `json:"detail"`
}

// reason returns whichever explanatory field Frigate populated.
func (r summaryResponse) reason() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Detail
}

// ResetSummaryTimer is called after each notification is sent.
// It tracks the start of activity and resets the idle timer.
// When the timer fires (no notifications for the configured duration),
// it requests a GenAI summary from Frigate covering the activity period.
func ResetSummaryTimer() {
	idleSeconds := config.ConfigData.Alerts.General.GenAI.SummaryIdleTime
	if idleSeconds <= 0 || !config.ConfigData.Alerts.General.GenAI.Enabled {
		return
	}

	summaryMu.Lock()
	defer summaryMu.Unlock()

	// Mark start of activity flurry
	if !flurryActive {
		flurryActive = true
		flurryStartTime = time.Now()
		log.Debug().
			Time("flurry_start", flurryStartTime).
			Msg("Activity flurry started")
	}

	// Reset or create the idle timer
	idleDuration := time.Duration(idleSeconds) * time.Second
	if summaryTimer != nil {
		summaryTimer.Stop()
	}
	summaryTimer = time.AfterFunc(idleDuration, onSummaryIdle)
}

func onSummaryIdle() {
	summaryMu.Lock()
	startTime := flurryStartTime
	flurryActive = false
	summaryMu.Unlock()

	endTime := time.Now()

	log.Debug().
		Time("start", startTime).
		Time("end", endTime).
		Msg("Activity idle - requesting GenAI summary from Frigate")

	concerns, err := activityHasConcerns(startTime, endTime)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Could not check reviews for concerns, requesting GenAI summary anyway")
	} else if !concerns {
		log.Debug().Msg("No review with GenAI concerns during activity, skipping summary")
		return
	}

	summary, err := requestFrigateSummary(startTime, endTime)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Failed to get GenAI summary from Frigate")
		return
	}

	if summary == "" {
		log.Debug().Msg("GenAI summary was empty, skipping notification")
		return
	}

	sendSummaryNotification(summary, startTime)
}

// windowReview is one review segment from the Frigate review API. Frigate
// keeps GenAI output that fails validation, so the concern fields are not
// guaranteed to be well-typed and are read as any.
type windowReview struct {
	Camera    string  `json:"camera"`
	StartTime float64 `json:"start_time"`
	Data      struct {
		Metadata *struct {
			Title                string `json:"title"`
			ShortSummary         string `json:"shortSummary"`
			PotentialThreatLevel any    `json:"potential_threat_level"`
			OtherConcerns        any    `json:"other_concerns"`
		} `json:"metadata"`
	} `json:"data"`
}

// windowReviews returns every review segment in the window. Without limit or
// reviewed params Frigate returns every segment, like summarize considers them.
func windowReviews(start, end time.Time) ([]windowReview, error) {
	url := fmt.Sprintf("%s/api/review?after=%d&before=%d",
		config.ConfigData.Frigate.Server,
		start.Unix(),
		end.Unix(),
	)

	response, err := util.HTTPGet(url, config.ConfigData.Frigate.Insecure, "", config.ConfigData.Frigate.Headers...)
	if err != nil {
		return nil, fmt.Errorf("review API request failed: %w", err)
	}

	var reviews []windowReview
	if err := json.Unmarshal(response, &reviews); err != nil {
		return nil, fmt.Errorf("failed to parse review response: %w", err)
	}
	return reviews, nil
}

// reviewsHaveConcerns reports whether any segment's GenAI metadata flags a
// threat or other concerns, mirroring Frigate's summarize predicate: only
// such segments reach the LLM; without any, Frigate returns a canned
// placeholder instead of a summary.
func reviewsHaveConcerns(reviews []windowReview) bool {
	for _, review := range reviews {
		meta := review.Data.Metadata
		if meta == nil {
			continue
		}
		if level, ok := meta.PotentialThreatLevel.(float64); ok && level > 0 {
			return true
		}
		// Frigate tests other_concerns for Python truthiness
		switch concerns := meta.OtherConcerns.(type) {
		case []any:
			if len(concerns) > 0 {
				return true
			}
		case string:
			if concerns != "" {
				return true
			}
		}
	}
	return false
}

// activityHasConcerns mirrors Frigate's summarize predicate: only review
// segments whose GenAI metadata flags a threat or other concerns reach the
// LLM; without any, Frigate returns a canned placeholder instead of a summary.
func activityHasConcerns(start, end time.Time) (bool, error) {
	reviews, err := windowReviews(start, end)
	if err != nil {
		return false, err
	}
	return reviewsHaveConcerns(reviews), nil
}

// postSummaryRequest POSTs to Frigate's summarize endpoint. This used to
// go through the shared util.HTTPPost helper, but that helper serves the
// notification senders, while Frigate-bound traffic must follow the
// Frigate request contract (login session, cookie jar, ignore-SSL flag)
// — so the call now rides on util.HTTPPostFrigate. The old call's
// observable behaviour is preserved: transport failures are retried up
// to the configured attempt count with the same pause between attempts,
// and a non-2xx status returns the response body together with an error
// naming the status code, so requestFrigateSummary can keep surfacing
// Frigate's own reason text exactly as before.
func postSummaryRequest(url string, insecure bool, headers ...map[string]string) ([]byte, error) {
	timeout := time.Duration(util.HTTPTimeout) * time.Second
	attempts := util.HTTPMaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var body []byte
	var status int
	var err error
	for attempt := 1; ; attempt++ {
		body, status, err = util.HTTPPostFrigate(url, insecure, nil, "", timeout, headers...)
		if err == nil {
			break
		}
		if attempt >= attempts {
			return nil, err
		}
		log.Warn().
			Int("attempt", attempt).
			Int("max_tries", attempts).
			Err(err).
			Msg("Summary request failed, retrying in 2 seconds...")
		time.Sleep(2 * time.Second)
	}
	if status < 200 || status > 299 {
		return body, fmt.Errorf("failed to send request, got status code %v", status)
	}
	return body, nil
}

func requestFrigateSummary(start, end time.Time) (string, error) {
	url := fmt.Sprintf("%s/api/review/summarize/start/%d/end/%d",
		config.ConfigData.Frigate.Server,
		start.Unix(),
		end.Unix(),
	)

	log.Debug().
		Str("url", url).
		Msg("Requesting GenAI summary from Frigate")

	response, err := postSummaryRequest(url, config.ConfigData.Frigate.Insecure, config.ConfigData.Frigate.Headers...)

	// Frigate answers non-2xx with a JSON body explaining why (no GenAI provider
	// with the descriptions role, the user lacks access to all cameras, ...).
	// That message is included in the returned error.
	var result summaryResponse
	parseErr := json.Unmarshal(response, &result)
	if err != nil {
		if parseErr == nil && result.reason() != "" {
			return "", fmt.Errorf("summary API request failed: %w: %s", err, result.reason())
		}
		return "", fmt.Errorf("summary API request failed: %w", err)
	}
	if parseErr != nil {
		return "", fmt.Errorf("failed to parse summary response: %w", parseErr)
	}

	if !result.Success {
		return "", fmt.Errorf("Frigate summary API error: %s", result.reason())
	}

	log.Debug().
		Int("summary_length", len(result.Summary)).
		Msg("Received GenAI summary from Frigate")

	return result.Summary, nil
}

func sendSummaryNotification(summary string, activityStart time.Time) {
	dispatchSummary(summaryEvent(summary, activityStart), nil, "活动总结")
}

// summaryEvent builds the synthetic event that carries a summary through the
// notification system. The summary goes into GenAISummary so templates render
// it naturally.
func summaryEvent(summary string, windowStart time.Time) models.Event {
	var event models.Event
	event.Extra.GenAITitle = "活动摘要"
	event.Extra.GenAISummary = summary
	event.Extra.CameraName = "All Cameras"
	event.Extra.FormattedTime = windowStart.String()
	if config.ConfigData.Alerts.General.TimeFormat != "" {
		event.Extra.FormattedTime = windowStart.Format(config.ConfigData.Alerts.General.TimeFormat)
	}
	event.Extra.LocalURL = config.ConfigData.Frigate.Server
	event.Extra.PublicURL = config.ConfigData.Frigate.PublicURL
	event.Extra.FrigateMajorVersion = config.Internal.FrigateVersion
	return event
}

// dispatchSummary sends one summary event to the chosen targets. An empty
// targets list means every enabled profile, including Bark (the idle path's
// old per-provider loop never listed it). Per-provider filters and routing
// rules do not apply — a summary covers all cameras, so a camera filter must
// not drop it. The dispatch bypasses the cooldown gate (a time-based digest
// must not be silently dropped by an unrelated event's cooldown) but counts
// toward the hourly cap like any alert the user sees, and is recorded in
// history as one entry whose detail names the kind (label). Returns the
// provider names it was sent to.
func dispatchSummary(event models.Event, targets []models.RoutingTarget, label string) []string {
	senders := allAlertSenders()
	if len(targets) > 0 {
		wanted := make(map[notifMeta]bool)
		for _, target := range targets {
			wanted[notifMeta{name: target.Provider, index: models.DerefInt(target.Index, -1)}] = true
		}
		var kept []alertSender
		for _, sender := range senders {
			if wanted[notifMeta{name: sender.name, index: sender.index}] ||
				wanted[notifMeta{name: sender.name, index: -1}] {
				kept = append(kept, sender)
			}
		}
		senders = kept
	}
	if len(senders) == 0 {
		return nil
	}
	if !hourlyCap.allow(config.ConfigData.Alerts.General.MaxNotificationsPerHour) {
		log.Info().Str("summary", label).Msg("Summary dropped - Hourly cap reached")
		RecordScheduledSummary(event, nil, label+" · 已达每小时上限")
		return nil
	}
	config.Internal.Status.LastNotification = time.Now()
	names := senderNames(senders)
	RecordScheduledSummary(event, names, label)
	deliveries := &sync.WaitGroup{}
	deliveries.Add(len(senders))
	for _, sender := range senders {
		go func(s alertSender) {
			defer deliveries.Done()
			s.send(event, nil)
		}(sender)
	}
	return names
}
