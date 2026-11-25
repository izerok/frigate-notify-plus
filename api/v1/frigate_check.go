package apiv1

import (
	"context"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/izerok/frigate-notify-plus/util"
	"github.com/rs/zerolog/log"
)

type FrigateCheckInput struct {
	Body struct {
		Server   string `json:"server"`
		Insecure bool   `json:"insecure,omitempty"`
	}
}

type FrigateCheckOutput struct {
	Body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Major   int    `json:"major"`
		Error   string `json:"error"`
	}
}

// PostFrigateCheck probes a Frigate server's /api/version endpoint and
// reports whether it answers and which major version it runs. It always
// answers 200 for a reachable-or-not Frigate server (the setup wizard
// displays the error text); only a missing server address is a 422.
func PostFrigateCheck(ctx context.Context, input *FrigateCheckInput) (*FrigateCheckOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/frigate_check").
		Str("method", "POST").
		Msg("Received API request")

	server := strings.TrimSuffix(strings.TrimSpace(input.Body.Server), "/")
	if server == "" {
		return nil, huma.Error422UnprocessableEntity("server must not be empty")
	}

	resp := &FrigateCheckOutput{}
	body, err := util.HTTPGet(server+"/api/version", input.Body.Insecure, "")
	if err != nil {
		resp.Body.OK = false
		resp.Body.Error = err.Error()
		return resp, nil
	}
	version := strings.TrimSpace(string(body))
	resp.Body.Version = version
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		resp.Body.OK = false
		resp.Body.Error = "unexpected version format: " + version
		return resp, nil
	}
	// Same parse as util.GetFrigateVersion: the 2nd dot component is the
	// major version (e.g. "0.18.2" -> 18).
	major, err := strconv.Atoi(parts[1])
	if err != nil {
		resp.Body.OK = false
		resp.Body.Error = "unexpected version format: " + version
		return resp, nil
	}
	resp.Body.OK = true
	resp.Body.Major = major
	return resp, nil
}
