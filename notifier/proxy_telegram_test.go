package notifier

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

// TestTelegramBotThroughProxy builds a bot exactly the way the telegram
// sender does when a proxy applies to the profile —
// tgbotapi.NewBotAPIWithClient with a proxyx.Client — and proves its API
// calls transit the proxy: the API host name used here is not
// resolvable, so nothing could arrive without the proxy.
func TestTelegramBotThroughProxy(t *testing.T) {
	var mu sync.Mutex
	var sendMessageSeen bool
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"Mock","username":"mockbot"}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			mu.Lock()
			sendMessageSeen = true
			mu.Unlock()
			io.WriteString(w, `{"ok":true,"result":{"message_id":42,"date":1,"chat":{"id":1,"type":"private"}}}`)
			return
		}
		io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer mockAPI.Close()

	proxy := newRecordingProxy(t, mockAPI.URL)

	proxyx.Configure(proxy.url(), nil, "")
	t.Cleanup(func() { proxyx.Configure("", nil, "") })

	profile := models.Telegram{Token: "token"} // Proxy "" = follow the global proxy
	if !proxyx.Configured(profile.Proxy) {
		t.Fatal("expected the global proxy to apply to the profile")
	}
	bot, err := tgbotapi.NewBotAPIWithClient(profile.Token, "http://telegram.test/bot%s/%s",
		proxyx.Client(profile.Proxy, 10*time.Second, false))
	if err != nil {
		t.Fatalf("NewBotAPIWithClient through proxy: %v", err)
	}
	if _, err := bot.Send(tgbotapi.NewMessage(1, "hello via proxy")); err != nil {
		t.Fatalf("send through proxy: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !sendMessageSeen {
		t.Error("mock Bot API never saw sendMessage")
	}
	if got := proxy.requests(); got < 2 { // getMe + sendMessage
		t.Errorf("proxy forwarded %d requests, want at least 2", got)
	}
}
