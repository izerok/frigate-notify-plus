package apiv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
)

// stubFrigateForProxyTest answers the version check config validation
// performs, so a PUT reaches the proxy validation with no other errors.
func stubFrigateForProxyTest(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "0.18.2")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func putConfigForProxyTest(t *testing.T, api humatest.TestAPI, frigateURL string, alerts string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{
		"config": {
			"frigate": {
				"server": %q,
				"mqtt": {"enabled": true, "server": "127.0.0.1", "port": 1883}
			},
			"alerts": %s
		},
		"skipbackup": true, "skipsave": true, "skipreload": true
	}`, frigateURL, alerts)
	resp := api.Put("/api/v1/config", bytes.NewReader([]byte(body)))
	return resp.Code, resp.Body.String()
}

// TestPutConfigProxyValidation proves a malformed proxy setting is a
// 422 naming the field, for both the global proxy URL and a per-profile
// override, and that a well-formed proxy config validates cleanly.
func TestPutConfigProxyValidation(t *testing.T) {
	origConfig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = origConfig })

	frigateURL := stubFrigateForProxyTest(t)

	_, api := humatest.New(t)
	Registerv1Routes(api)

	// Malformed global proxy URL.
	code, body := putConfigForProxyTest(t, api, frigateURL, `{
		"general": {"cooldown": 0, "nosnap": "allow", "snap_bbox": false, "snap_timestamp": false, "snap_crop": false, "notify_once": false, "notify_detections": false, "max_snap_retry": 10, "proxy": {"url": "ftp://proxy.example.com:21"}},
		"webhook": [{"enabled": true, "server": "http://127.0.0.1:18081/hook"}]
	}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad global proxy: expected HTTP 422, got %d: %s", code, body)
	}
	if !strings.Contains(body, "alerts.general.proxy.url") {
		t.Errorf("422 body should name alerts.general.proxy.url, got: %s", body)
	}

	// Malformed per-profile override.
	code, body = putConfigForProxyTest(t, api, frigateURL, `{
		"webhook": [{"enabled": true, "server": "http://127.0.0.1:18081/hook", "proxy": "http://proxy.example.com/path"}]
	}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad profile proxy: expected HTTP 422, got %d: %s", code, body)
	}
	if !strings.Contains(body, "alerts.webhook.0.proxy") {
		t.Errorf("422 body should name alerts.webhook.0.proxy, got: %s", body)
	}

	// Well-formed global + profile settings are accepted.
	code, body = putConfigForProxyTest(t, api, frigateURL, `{
		"general": {"cooldown": 0, "nosnap": "allow", "snap_bbox": false, "snap_timestamp": false, "snap_crop": false, "notify_once": false, "notify_detections": false, "max_snap_retry": 10, "proxy": {"url": "http://user:pass@192.168.1.10:7890", "no_proxy": ["192.168.0.0/16", "localhost"]}},
		"webhook": [{"enabled": true, "server": "http://127.0.0.1:18081/hook", "proxy": "direct"}]
	}`)
	if code != http.StatusAccepted {
		t.Fatalf("valid proxy config: expected HTTP 202, got %d: %s", code, body)
	}
	var decoded struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil || decoded.Status != "ok" {
		t.Errorf("valid proxy config: unexpected body %s", body)
	}
}
