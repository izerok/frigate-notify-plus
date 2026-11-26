package config

import (
	"strings"
	"testing"

	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

func TestValidateProxy(t *testing.T) {
	// A valid setup: global URL with credentials, a bypass list, and
	// profile overrides in each form (follow, direct, own URL).
	config := Config{}
	config.Alerts.General.Proxy = models.Proxy{
		URL:     "http://user:pass@192.168.1.10:7890",
		NoProxy: []string{"192.168.0.0/16", "localhost", "gotify.lan"},
	}
	config.Alerts.Telegram = []models.Telegram{{Proxy: ""}, {Proxy: "direct"}, {Proxy: "socks5://10.0.0.1:1080"}}
	if results := config.validateProxy(); len(results) != 0 {
		t.Errorf("Expected no errors for a valid proxy config, got: %v", results)
	}

	// Malformed global URL is reported with its field path.
	config = Config{}
	config.Alerts.General.Proxy.URL = "ftp://proxy.example.com:21"
	results := config.validateProxy()
	if len(results) != 1 || !strings.HasPrefix(results[0], "alerts.general.proxy.url: ") {
		t.Errorf("Expected one alerts.general.proxy.url error, got: %v", results)
	}

	// A bypass entry that looks like a URL is rejected.
	config = Config{}
	config.Alerts.General.Proxy.NoProxy = []string{"http://gotify.lan"}
	results = config.validateProxy()
	if len(results) != 1 || !strings.HasPrefix(results[0], "alerts.general.proxy.no_proxy.0: ") {
		t.Errorf("Expected one alerts.general.proxy.no_proxy.0 error, got: %v", results)
	}

	// A malformed profile override names the profile, even disabled.
	config = Config{}
	config.Alerts.Webhook = []models.Webhook{{Proxy: "http://proxy.example.com/path"}}
	results = config.validateProxy()
	if len(results) != 1 || !strings.HasPrefix(results[0], "alerts.webhook.0.proxy: ") {
		t.Errorf("Expected one alerts.webhook.0.proxy error, got: %v", results)
	}

	// Every provider's override is checked.
	config = Config{}
	config.Alerts.Bark = []models.Bark{{Proxy: "gopher://x"}}
	config.Alerts.SMTP = []models.SMTP{{Proxy: "gopher://x"}}
	results = config.validateProxy()
	if len(results) != 2 {
		t.Errorf("Expected errors for both bark and smtp profiles, got: %v", results)
	}
}

// Validation must not publish proxy settings: it also runs on candidate
// configs that a save then rejects, and a rejected save must not change how
// the running app delivers notifications.
func TestValidateProxyDoesNotPublishRuntimeState(t *testing.T) {
	origConfig := ConfigData
	t.Cleanup(func() {
		ConfigData = origConfig
		proxyx.Configure("", nil, "")
	})
	proxyx.Configure("", nil, "")

	cfg := Config{}
	cfg.Alerts.General.Proxy = models.Proxy{URL: "http://127.0.0.1:7890"}
	if results := cfg.validateProxy(); len(results) != 0 {
		t.Fatalf("unexpected validation errors: %v", results)
	}
	if proxyx.Configured("") {
		t.Fatal("validateProxy published proxy state for a config that is not in use")
	}

	ConfigData = cfg
	ApplyProxyRuntime()
	if !proxyx.Configured("") {
		t.Fatal("ApplyProxyRuntime did not publish the accepted config's proxy")
	}
}
