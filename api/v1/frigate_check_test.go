package apiv1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
)

func postFrigateCheck(t *testing.T, api humatest.TestAPI, server string) (int, map[string]interface{}) {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{"server": server})
	resp := api.Post("/api/v1/frigate_check", bytes.NewReader(payload))
	var body map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	return resp.Code, body
}

func TestPostFrigateCheckOK(t *testing.T) {
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("unexpected path %q, want /api/version", r.URL.Path)
		}
		io.WriteString(w, "0.18.2")
	}))
	defer frigate.Close()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, body := postFrigateCheck(t, api, frigate.URL)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %v", code, body)
	}
	if body["ok"] != true {
		t.Errorf("ok = %v, want true", body["ok"])
	}
	if body["version"] != "0.18.2" {
		t.Errorf("version = %v, want 0.18.2", body["version"])
	}
	if major, ok := body["major"].(float64); !ok || major != 18 {
		t.Errorf("major = %v, want 18", body["major"])
	}
	if body["error"] != "" {
		t.Errorf("error = %v, want empty", body["error"])
	}
}

func TestPostFrigateCheckError(t *testing.T) {
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer frigate.Close()

	_, api := humatest.New(t)
	Registerv1Routes(api)

	// A failing Frigate server is still a 200 with ok=false, never a 4xx/5xx.
	code, body := postFrigateCheck(t, api, frigate.URL)
	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %v", code, body)
	}
	if body["ok"] != false {
		t.Errorf("ok = %v, want false", body["ok"])
	}
	if body["error"] == "" || body["error"] == nil {
		t.Errorf("error = %v, want a non-empty error", body["error"])
	}
}

func TestPostFrigateCheckEmptyServer(t *testing.T) {
	_, api := humatest.New(t)
	Registerv1Routes(api)

	code, _ := postFrigateCheck(t, api, "")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for an empty server, got %d", code)
	}
}
