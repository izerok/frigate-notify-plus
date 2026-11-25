package apiv1

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/rs/zerolog/log"
	yml "gopkg.in/yaml.v3"
)

// GetConfigYAML returns the running configuration as YAML, using the same
// keys as config.yml. Used by the Web UI advanced editor.
func GetConfigYAML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := yml.Marshal(&config.ConfigData)
	if err != nil {
		log.Error().Err(err).Msg("Unable to marshal config to YAML")
		http.Error(w, "unable to marshal config", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(data)
}

// PutConfigYAML replaces the running configuration from a YAML body, then
// validates, saves and reloads it like PUT /api/v1/config does.
func PutConfigYAML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unable to read request body", http.StatusBadRequest)
		return
	}
	var newConfig config.Config
	if err := yml.Unmarshal(body, &newConfig); err != nil {
		writeYAMLResult(w, http.StatusUnprocessableEntity, "validation error", []string{err.Error()})
		return
	}
	if errs := newConfig.Validate(); len(errs) > 0 {
		writeYAMLResult(w, http.StatusUnprocessableEntity, "validation error", errs)
		return
	}
	go reloadCfg(newConfig, false, false)
	writeYAMLResult(w, http.StatusAccepted, "ok", nil)
}

func writeYAMLResult(w http.ResponseWriter, status int, result string, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{"status": result}
	if len(errs) > 0 {
		resp["errors"] = errs
	}
	_ = json.NewEncoder(w).Encode(resp)
}
