package apiv1

import (
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

func setupFrigateMetaTest(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	frigate := httptest.NewServer(handler)
	orig := config.ConfigData
	config.ConfigData.Frigate.Server = frigate.URL
	config.ConfigData.Frigate.Insecure = false
	config.ConfigData.Frigate.Headers = nil
	return frigate, func() { config.ConfigData = orig }
}

func getFrigateMeta(t *testing.T, api humatest.TestAPI) (int, map[string]interface{}) {
	t.Helper()
	resp := api.Get("/api/v1/frigate_meta")
	var body map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	return resp.Code, body
}

func TestGetFrigateMetaOK(t *testing.T) {
	frigate, restore := setupFrigateMetaTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/config":
			// mqtt password is the Frigate 0.18 redaction sentinel: it must
			// never be echoed back to the client.
			io.WriteString(w, `{
				"cameras": {
					"front_door": {"zones": {"yard": {}, "driveway": {}}},
					"back_yard": {"zones": {"patio": {}}}
				},
				"objects": {"track": ["person", "car", "dog"]},
				"model": {"labelmap": {}},
				"mqtt": {"enabled": true, "host": "127.0.0.1", "port": 1883, "user": "frigate", "password": "__FRIGATE_SAVED_CREDENTIAL__"}
			}`)
		case "/api/events":
			if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("include_thumbnails") != "0" {
				t.Errorf("events query = %q, want limit=200&include_thumbnails=0", r.URL.RawQuery)
			}
			io.WriteString(w, `[
				{"id": "e1", "sub_label": "delivery"},
				{"id": "e2", "sub_label": "delivery"},
				{"id": "e3", "sub_label": "mailman"},
				{"id": "e4", "sub_label": ""},
				{"id": "e5", "sub_label": null}
			]`)
		case "/api/version":
			io.WriteString(w, "0.18.2")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer frigate.Close()
	defer restore()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := getFrigateMeta(t, api)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %v", code, body)
	}

	cameras, ok := body["cameras"].([]interface{})
	if !ok || len(cameras) != 2 {
		t.Fatalf("cameras = %v, want 2 cameras", body["cameras"])
	}
	first, _ := cameras[0].(map[string]interface{})
	if first["name"] != "back_yard" {
		t.Errorf("cameras[0].name = %v, want back_yard (sorted)", first["name"])
	}
	if zones, _ := first["zones"].([]interface{}); len(zones) != 1 || zones[0] != "patio" {
		t.Errorf("cameras[0].zones = %v, want [patio]", first["zones"])
	}
	second, _ := cameras[1].(map[string]interface{})
	if second["name"] != "front_door" {
		t.Errorf("cameras[1].name = %v, want front_door", second["name"])
	}
	if zones, _ := second["zones"].([]interface{}); len(zones) != 2 || zones[0] != "driveway" || zones[1] != "yard" {
		t.Errorf("cameras[1].zones = %v, want [driveway yard] (sorted)", second["zones"])
	}

	zones, _ := body["zones"].([]interface{})
	if len(zones) != 3 || zones[0] != "driveway" || zones[1] != "patio" || zones[2] != "yard" {
		t.Errorf("zones = %v, want [driveway patio yard]", body["zones"])
	}

	labels, _ := body["labels"].([]interface{})
	if len(labels) != 3 || labels[0] != "person" || labels[1] != "car" || labels[2] != "dog" {
		t.Errorf("labels = %v, want [person car dog]", body["labels"])
	}

	sublabels, _ := body["sublabels"].([]interface{})
	if len(sublabels) != 2 || sublabels[0] != "delivery" || sublabels[1] != "mailman" {
		t.Errorf("sublabels = %v, want [delivery mailman] (deduped, empties dropped)", body["sublabels"])
	}

	mqtt, ok := body["mqtt"].(map[string]interface{})
	if !ok {
		t.Fatalf("mqtt = %v, want an object", body["mqtt"])
	}
	if mqtt["enabled"] != true || mqtt["host"] != "127.0.0.1" || mqtt["port"] != float64(1883) || mqtt["user"] != "frigate" {
		t.Errorf("mqtt = %v, want enabled/host/port/user", mqtt)
	}
	if _, present := mqtt["password"]; present {
		t.Errorf("mqtt must not contain a password field, got %v", mqtt)
	}

	if body["version"] != "0.18.2" {
		t.Errorf("version = %v, want 0.18.2", body["version"])
	}
}

func TestGetFrigateMetaLabelsFallback(t *testing.T) {
	// No objects.track: fall back to the model labelmap values.
	frigate, restore := setupFrigateMetaTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/config":
			io.WriteString(w, `{
				"cameras": {},
				"objects": {"track": []},
				"model": {"labelmap": {"0": "person", "2": "car"}},
				"mqtt": {"enabled": false}
			}`)
		case "/api/events":
			io.WriteString(w, `[]`)
		case "/api/version":
			io.WriteString(w, "0.18.2")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer frigate.Close()
	defer restore()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := getFrigateMeta(t, api)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %v", code, body)
	}
	labels, _ := body["labels"].([]interface{})
	if len(labels) != 2 || labels[0] != "car" || labels[1] != "person" {
		t.Errorf("labels = %v, want [car person] from model labelmap", body["labels"])
	}
	mqtt, _ := body["mqtt"].(map[string]interface{})
	if mqtt["enabled"] != false {
		t.Errorf("mqtt.enabled = %v, want false", mqtt["enabled"])
	}
}

func TestGetFrigateMetaSubLabelsCapped(t *testing.T) {
	// 60 distinct sub-labels: only the first 50 (sorted) are returned.
	frigate, restore := setupFrigateMetaTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/config":
			io.WriteString(w, `{"cameras": {}, "mqtt": {}}`)
		case "/api/events":
			events := make([]string, 0, 60)
			for i := 0; i < 60; i++ {
				events = append(events, fmt.Sprintf(`{"id": "e%d", "sub_label": "sub-%02d"}`, i, i))
			}
			io.WriteString(w, "["+strings.Join(events, ",")+"]")
		case "/api/version":
			io.WriteString(w, "0.18.2")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer frigate.Close()
	defer restore()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := getFrigateMeta(t, api)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %v", code, body)
	}
	sublabels, _ := body["sublabels"].([]interface{})
	if len(sublabels) != 50 {
		t.Fatalf("sublabels = %d entries, want 50 (capped)", len(sublabels))
	}
	if sublabels[0] != "sub-00" || sublabels[49] != "sub-49" {
		t.Errorf("sublabels range = %v..%v, want sub-00..sub-49", sublabels[0], sublabels[49])
	}
}

func TestGetFrigateMetaDegraded(t *testing.T) {
	// Frigate unreachable: still 200, with empty arrays and no mqtt/version.
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := frigate.URL
	frigate.Close()

	orig := config.ConfigData
	config.ConfigData.Frigate.Server = url
	config.ConfigData.Frigate.Insecure = false
	config.ConfigData.Frigate.Headers = nil
	defer func() { config.ConfigData = orig }()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := getFrigateMeta(t, api)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for unreachable Frigate, got %d: %v", code, body)
	}
	for _, key := range []string{"cameras", "zones", "labels", "sublabels"} {
		arr, ok := body[key].([]interface{})
		if !ok || len(arr) != 0 {
			t.Errorf("%s = %v, want an empty array", key, body[key])
		}
	}
	if _, present := body["mqtt"]; present {
		t.Errorf("mqtt = %v, want omitted when Frigate is unreachable", body["mqtt"])
	}
	if _, present := body["version"]; present {
		t.Errorf("version = %v, want omitted when Frigate is unreachable", body["version"])
	}
}

func TestGetFrigateMetaNoServer(t *testing.T) {
	// No Frigate server configured (setup mode): still 200, empty arrays.
	orig := config.ConfigData
	config.ConfigData.Frigate.Server = ""
	defer func() { config.ConfigData = orig }()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := getFrigateMeta(t, api)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200 with no server configured, got %d: %v", code, body)
	}
	for _, key := range []string{"cameras", "zones", "labels", "sublabels"} {
		arr, ok := body[key].([]interface{})
		if !ok || len(arr) != 0 {
			t.Errorf("%s = %v, want an empty array", key, body[key])
		}
	}
}
