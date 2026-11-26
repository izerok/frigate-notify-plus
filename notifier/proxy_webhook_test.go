package notifier

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util"
	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

// recordingProxy is a minimal HTTP forward proxy for tests: it counts
// the requests it forwards and relays them to a fixed target server,
// whatever host the absolute-form request URI names (the names used in
// tests, e.g. hooks.test, are intentionally not resolvable — only the
// proxy can deliver them).
type recordingProxy struct {
	server *httptest.Server
	target string

	mu    sync.Mutex
	count int
}

func newRecordingProxy(t *testing.T, target string) *recordingProxy {
	t.Helper()
	p := &recordingProxy{target: target}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.count++
		p.mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		forward, err := http.NewRequest(r.Method, p.target+r.URL.Path, strings.NewReader(string(body)))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		for name, values := range r.Header {
			for _, value := range values {
				forward.Header.Add(name, value)
			}
		}
		resp, err := http.DefaultClient.Do(forward)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *recordingProxy) url() string { return p.server.URL }

func (p *recordingProxy) requests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

// TestSendWebhookThroughProxy proves the webhook sender honours the
// global proxy: the target is reachable only through the proxy, and a
// no_proxy entry for the target host switches the delivery back to a
// direct connection.
func TestSendWebhookThroughProxy(t *testing.T) {
	var targetMu sync.Mutex
	var targetPosts int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetMu.Lock()
		targetPosts++
		targetMu.Unlock()
		io.WriteString(w, "ok")
	}))
	defer target.Close()

	proxy := newRecordingProxy(t, target.URL)

	origConfig := config.ConfigData
	origStatus := config.Internal.Status.Notifications.Webhook
	origAttempts := util.HTTPMaxAttempts
	t.Cleanup(func() {
		config.ConfigData = origConfig
		config.Internal.Status.Notifications.Webhook = origStatus
		util.HTTPMaxAttempts = origAttempts
		proxyx.Configure("", nil, "")
	})
	util.HTTPMaxAttempts = 1 // the bypass case below fails fast by design
	config.ConfigData = config.Config{}
	config.ConfigData.Alerts.Webhook = []models.Webhook{
		{AlertCommon: models.AlertCommon{Enabled: true}, Server: "http://hooks.test/hook", Method: "POST"},
	}
	config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 1)

	// With a global proxy configured, the delivery transits the proxy.
	proxyx.Configure(proxy.url(), nil, "")
	SendWebhook(models.Event{}, notifMeta{name: "webhook", index: 0})
	if got := proxy.requests(); got != 1 {
		t.Fatalf("proxy forwarded %d requests, want 1", got)
	}
	if targetPosts != 1 {
		t.Fatalf("target received %d posts, want 1", targetPosts)
	}
	if config.Internal.Status.Notifications.Webhook[0].Sent != 1 {
		t.Fatal("expected the proxied webhook send to succeed")
	}

	// With the target host on the bypass list, the proxy is skipped.
	// (hooks.test does not resolve, so this delivery cannot arrive; what
	// matters is that the proxy sees nothing new.)
	proxyx.Configure(proxy.url(), []string{"hooks.test"}, "")
	SendWebhook(models.Event{}, notifMeta{name: "webhook", index: 0})
	if got := proxy.requests(); got != 1 {
		t.Fatalf("proxy forwarded %d requests after no_proxy bypass, want still 1", got)
	}
	if targetPosts != 1 {
		t.Fatalf("target received %d posts after bypass, want still 1", targetPosts)
	}
}
