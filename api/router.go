package api

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	apiv1 "github.com/izerok/frigate-notify-plus/api/v1"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/rs/zerolog/log"
)

// RunAPIServer starts the REST API server and serves the single-page Web UI
// console from the embedded webui files on the same port.
func RunAPIServer(webuiFS fs.FS) error {

	router := http.NewServeMux()

	// Configure API
	apiConfig := huma.DefaultConfig("Frigate-Notify-Plus", config.Internal.AppVersion)
	apiConfig.Info.License = &huma.License{Name: "MIT",
		URL: "https://github.com/izerok/frigate-notify-plus/blob/main/LICENSE"}
	apiConfig.Info.Contact = &huma.Contact{Name: "frigate-notify-plus",
		URL: "https://github.com/izerok/frigate-notify-plus",
	}
	api := humago.New(router, apiConfig)

	apiv1.Registerv1Routes(api)

	// Raw YAML config endpoints (plain handlers: huma has no YAML body type).
	router.HandleFunc("/api/v1/config/yaml", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			apiv1.GetConfigYAML(w, r)
		case http.MethodPut:
			apiv1.PutConfigYAML(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Web UI console (LAN-only, no authentication by design).
	var uiHTML []byte
	var err error
	if webuiFS != nil {
		uiHTML, err = fs.ReadFile(webuiFS, "index.html")
	} else {
		err = fs.ErrNotExist
	}
	if err != nil {
		log.Warn().Err(err).Msg("Web UI files not embedded; console will be unavailable")
		uiHTML = []byte("<h1>Frigate Notify Plus</h1><p>Web UI is unavailable in this build.</p>")
	}
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/ui") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uiHTML)
	})

	log.Debug().Msg("Starting API server...")
	listenAddr := fmt.Sprintf("0.0.0.0:%v", config.ConfigData.App.API.Port)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	go http.Serve(listener, router)
	return nil
}
