package notifier

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	tgbotapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// setupThreatTemplates installs minimal message templates so the senders'
// renderMessage calls work, and restores the previous TemplateFiles.
func setupThreatTemplates(t *testing.T) {
	t.Helper()
	orig := TemplateFiles
	TemplateFiles = fstest.MapFS{
		"templates/plaintext.template": {Data: []byte("plain {{ .Label }}")},
		"templates/markdown.template":  {Data: []byte("md {{ .Label }}")},
		"templates/html.template":      {Data: []byte("<p>{{ .Label }}</p>")},
	}
	t.Cleanup(func() { TemplateFiles = orig })
}

// setupThreatPriority installs the default threat-priority mapping in the
// global config. Callers replace config.ConfigData wholesale first.
func setupThreatPriority() {
	config.ConfigData.Alerts.General.ThreatPriority = models.ThreatPriority{
		NtfyNormal:               3,
		NtfySuspicious:           4,
		NtfyCritical:             5,
		GotifyNormal:             5,
		GotifySuspicious:         8,
		GotifyCritical:           10,
		PushoverNormal:           0,
		PushoverSuspicious:       1,
		PushoverCritical:         2,
		TelegramSilentNormal:     true,
		TelegramSilentSuspicious: true,
		TelegramSilentCritical:   false,
	}
}

func threatEvent(level string) models.Event {
	event := models.Event{ID: "evt1", Camera: "front_door", Label: "person"}
	event.Extra.GenAIThreatLevel = level
	// Pushover rejects a URLTitle with an empty URL, so the event needs a link.
	event.Extra.EventLink = "http://public.test/api/events/evt1/clip.mp4"
	return event
}

func TestSendNtfyThreatPriority(t *testing.T) {
	setupThreatTemplates(t)

	var gotPriority string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPriority = r.Header.Get("X-Priority")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Ntfy
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Ntfy = origStatus
	})
	config.ConfigData = config.Config{}
	config.ConfigData.Alerts.General.Title = "Test"
	setupThreatPriority()
	config.ConfigData.Alerts.Ntfy = []models.Ntfy{
		{AlertCommon: models.AlertCommon{Enabled: true}, Server: srv.URL, Topic: "test"},
	}
	config.Internal.Status.Notifications.Ntfy = make([]models.NotifierStatus, 1)

	tests := []struct {
		threat string
		want   string
	}{
		{"", "3"},       // no GenAI data -> level 0 -> Normal value (ntfy default)
		{"Normal", "3"}, // explicit Normal
		{"Suspicious", "4"},
		{"Critical", "5"},
	}
	for _, tc := range tests {
		t.Run(tc.threat, func(t *testing.T) {
			SendNtfyPush(threatEvent(tc.threat), nil, notifMeta{name: "ntfy", index: 0})
			if gotPriority != tc.want {
				t.Errorf("X-Priority = %q, want %q (threat %q)", gotPriority, tc.want, tc.threat)
			}
			if config.Internal.Status.Notifications.Ntfy[0].Sent == 0 {
				t.Error("expected the ntfy send to succeed (sent counter still 0)")
			}
		})
	}
}

func TestSendGotifyThreatPriority(t *testing.T) {
	setupThreatTemplates(t)

	var gotPriority int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload gotifyPayload
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		gotPriority = payload.Priority
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Gotify
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Gotify = origStatus
	})
	config.ConfigData = config.Config{}
	config.ConfigData.Frigate.Server = "http://frigate.test"
	config.ConfigData.Frigate.PublicURL = "http://public.test"
	config.ConfigData.Alerts.General.Title = "Test"
	setupThreatPriority()
	config.Internal.Status.Notifications.Gotify = make([]models.NotifierStatus, 1)

	tests := []struct {
		name            string
		profilePriority int
		threat          string
		want            int
	}{
		{"no genai maps to normal", 0, "", 5},
		{"suspicious maps to 8", 0, "Suspicious", 8},
		{"critical maps to 10", 0, "Critical", 10},
		{"profile priority wins over mapping", 7, "Critical", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.ConfigData.Alerts.Gotify = []models.Gotify{
				{AlertCommon: models.AlertCommon{Enabled: true}, Server: srv.URL, Token: "tok", Priority: tc.profilePriority},
			}
			SendGotifyPush(threatEvent(tc.threat), notifMeta{name: "gotify", index: 0})
			if gotPriority != tc.want {
				t.Errorf("gotify priority = %d, want %d", gotPriority, tc.want)
			}
			if config.Internal.Status.Notifications.Gotify[0].Sent == 0 {
				t.Error("expected the gotify send to succeed (sent counter still 0)")
			}
		})
	}
}

func TestSendPushoverThreatPriority(t *testing.T) {
	setupThreatTemplates(t)

	// The pushover sender posts to a package-level endpoint; point it at
	// a mock server that records the form-encoded priority.
	var gotPriority string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		gotPriority = values.Get("priority")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":1,"request":"req-1"}`)
	}))
	defer srv.Close()

	origEndpoint := pushoverAPIEndpoint
	pushoverAPIEndpoint = srv.URL
	t.Cleanup(func() { pushoverAPIEndpoint = origEndpoint })

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Pushover
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Pushover = origStatus
	})
	config.ConfigData = config.Config{}
	config.ConfigData.Alerts.General.Title = "Test"
	setupThreatPriority()
	config.Internal.Status.Notifications.Pushover = make([]models.NotifierStatus, 1)

	const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 30 chars, matches pushover's token format
	const userkey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	tests := []struct {
		name            string
		profilePriority int
		threat          string
		want            string
	}{
		{"no genai maps to normal", 0, "", "0"},
		{"suspicious maps to 1", 0, "Suspicious", "1"},
		{"critical maps to 2", 0, "Critical", "2"},
		{"profile priority wins over mapping", -1, "Critical", "-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.ConfigData.Alerts.Pushover = []models.Pushover{
				{
					AlertCommon: models.AlertCommon{Enabled: true},
					Token:       token, Userkey: userkey,
					Priority: tc.profilePriority,
					// Emergency priority (2) requires retry/expire.
					Retry: 60, Expire: 3600,
				},
			}
			SendPushoverMessage(threatEvent(tc.threat), nil, notifMeta{name: "pushover", index: 0})
			if gotPriority != tc.want {
				t.Errorf("pushover priority = %q, want %q", gotPriority, tc.want)
			}
			if config.Internal.Status.Notifications.Pushover[0].Sent == 0 {
				t.Error("expected the pushover send to succeed (sent counter still 0)")
			}
		})
	}
}

func TestSendTelegramSilentOnlyWithGenAI(t *testing.T) {
	setupThreatTemplates(t)

	tg, reqs, mu := newMockTelegram(t)
	defer tg.Close()

	origNewBot := newTelegramBot
	newTelegramBot = func(profile models.Telegram) (*tgbotapi.BotAPI, error) {
		return tgbotapi.NewBotAPIWithAPIEndpoint(profile.Token, tg.URL+"/bot%s/%s")
	}
	t.Cleanup(func() { newTelegramBot = origNewBot })

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Telegram
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Telegram = origStatus
	})
	config.ConfigData = config.Config{}
	config.ConfigData.Alerts.General.Title = "Test"
	setupThreatPriority()
	config.ConfigData.Alerts.Telegram = []models.Telegram{
		{AlertCommon: models.AlertCommon{Enabled: true}, ChatID: 1, Token: "token"},
	}
	config.Internal.Status.Notifications.Telegram = make([]models.NotifierStatus, 1)

	// photoOnly returns whether the sendPhoto request carried
	// disable_notification=true.
	photoSilent := func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range *reqs {
			if r.endpoint == "sendPhoto" {
				return strings.Contains(r.body, "disable_notification")
			}
		}
		return false
	}
	resetReqs := func() {
		mu.Lock()
		*reqs = (*reqs)[:0]
		mu.Unlock()
	}

	event := models.Event{ID: "evt1", Camera: "front_door", Label: "person", HasSnapshot: true}

	t.Run("suspicious with genai is silent", func(t *testing.T) {
		resetReqs()
		e := event
		e.Extra.GenAIThreatLevel = "Suspicious"
		SendTelegramMessage(e, bytes.NewReader([]byte("FAKE-SNAPSHOT")), notifMeta{name: "telegram", index: 0})
		if !photoSilent() {
			t.Error("expected sendPhoto to carry disable_notification for a Suspicious GenAI event")
		}
	})

	t.Run("no genai data is never silent", func(t *testing.T) {
		resetReqs()
		e := event // no GenAIThreatLevel, even though silent-normal is true
		SendTelegramMessage(e, bytes.NewReader([]byte("FAKE-SNAPSHOT")), notifMeta{name: "telegram", index: 0})
		if photoSilent() {
			t.Error("expected no disable_notification without GenAI data")
		}
	})

	t.Run("suspicious with silent flag off is not silent", func(t *testing.T) {
		resetReqs()
		config.ConfigData.Alerts.General.ThreatPriority.TelegramSilentSuspicious = false
		e := event
		e.Extra.GenAIThreatLevel = "Suspicious"
		SendTelegramMessage(e, bytes.NewReader([]byte("FAKE-SNAPSHOT")), notifMeta{name: "telegram", index: 0})
		if photoSilent() {
			t.Error("expected no disable_notification when the Suspicious silent flag is off")
		}
	})
}
