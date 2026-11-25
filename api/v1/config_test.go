package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
)

func TestGetConfig(t *testing.T) {
	_, api := humatest.New(t)

	Registerv1Routes(api)

	resp := api.Get("/api/v1/config")

	if resp.Code != http.StatusOK {
		t.Error("Expected HTTP 200, got ", resp.Code)
	}
}

func TestGetConfigDefaults(t *testing.T) {
	_, api := humatest.New(t)

	Registerv1Routes(api)

	resp := api.Get("/api/v1/config")
	if resp.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.Code)
	}
	var body struct {
		Config   config.Config `json:"config"`
		Defaults config.Config `json:"defaults"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unable to decode config response: %v", err)
	}
	// The defaults in the response must deep-equal DefaultConfig after a
	// JSON round-trip (nil slices/maps survive as null, so this holds).
	if !reflect.DeepEqual(body.Defaults, config.DefaultConfig) {
		t.Errorf("defaults = %+v, want DefaultConfig %+v", body.Defaults, config.DefaultConfig)
	}
}

func TestPutConfig(t *testing.T) {
	_, api := humatest.New(t)

	Registerv1Routes(api)

	newconfig := `{
   "config":{
      "frigate":{
         "server":"http://192.0.2.10:5000",
         "mqtt":{
            "enabled": true
         }
      },
      "alerts":{
      }
   },
	  "skipvalidate": true,
	  "skipbackup": true,
	  "skipsave": true,
	  "skipreload": true
}`

	resp := api.Put("/api/v1/config", bytes.NewReader([]byte(newconfig)))

	if resp.Code != http.StatusAccepted {
		t.Error("Expected HTTP 202, got ", resp.Code)
	}
}
