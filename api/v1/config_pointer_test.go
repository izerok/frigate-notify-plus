package apiv1

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

// TestPutConfigKeepsExplicitZeros is the regression test for the huma
// zero-value default trap (see models/pointers.go): huma applies a field's
// `default:` tag to ANY zero value decoded from a JSON request body, so a
// config PUT with an explicit routing target index 0, escalation
// min_threat 0, or schedule enabled false used to arrive as index -1,
// min_threat 1, and enabled true. The pointer fields keep explicit zeros.
//
// The PUT runs with skipvalidate (to isolate huma's decoding from
// Validate's normalization) and skipsave, but NOT skipreload, so the
// decoded config lands in config.ConfigData, which this test polls.
func TestPutConfigKeepsExplicitZeros(t *testing.T) {
	_, api := humatest.New(t)
	Registerv1Routes(api)

	orig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = orig })

	body := `{
   "config":{
      "frigate":{ "server":"http://192.0.2.10:5000" },
      "alerts":{
         "webhook":[{"enabled":true,"server":"http://127.0.0.1:18081/hook"}],
         "routing":{
            "rules":[{"name":"zero-rule","cameras":["front_door"],"targets":[{"provider":"webhook","index":0}]}],
            "escalation":[{"min_threat":0,"targets":[{"provider":"webhook","index":0}]}]
         },
         "general":{
            "nosnap":"allow","snap_bbox":false,"snap_timestamp":false,"snap_crop":false,"snap_hires":false,
            "max_snap_retry":10,"notify_once":false,"notify_detections":false,
            "genai":{"enabled":true,"update_notif":true,"schedules":[{"enabled":false,"time":"20:00","window":"today","targets":[{"provider":"webhook","index":0}]}]}
         }
      }
   },
	  "skipvalidate": true,
	  "skipbackup": true,
	  "skipsave": true,
	  "skipreload": false
}`
	resp := api.Put("/api/v1/config", bytes.NewReader([]byte(body)))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("PUT /api/v1/config = %d, want 202: %s", resp.Code, resp.Body.String())
	}
	// reloadCfg runs in a goroutine; poll for the decoded values.
	deadline := time.Now().Add(5 * time.Second)
	for {
		cfg := config.ConfigData
		if len(cfg.Alerts.Routing.Rules) > 0 && len(cfg.Alerts.Routing.Escalation) > 0 &&
			len(cfg.Alerts.General.GenAI.Schedules) > 0 {
			rule := cfg.Alerts.Routing.Rules[0]
			esc := cfg.Alerts.Routing.Escalation[0]
			sched := cfg.Alerts.General.GenAI.Schedules[0]
			if len(rule.Targets) > 0 && len(esc.Targets) > 0 && len(sched.Targets) > 0 {
				if got := models.DerefInt(rule.Targets[0].Index, 99); got != 0 {
					t.Fatalf("routing target index 0 became %d after PUT", got)
				}
				if got := models.DerefInt(esc.MinThreat, 99); got != 0 {
					t.Fatalf("escalation min_threat 0 became %d after PUT", got)
				}
				if got := models.DerefInt(esc.Targets[0].Index, 99); got != 0 {
					t.Fatalf("escalation target index 0 became %d after PUT", got)
				}
				if got := models.DerefBool(sched.Enabled, true); got != false {
					t.Fatalf("schedule enabled false became %v after PUT", got)
				}
				if got := models.DerefInt(sched.Targets[0].Index, 99); got != 0 {
					t.Fatalf("schedule target index 0 became %d after PUT", got)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("config reload did not land within 5s")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
