package apiv1

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/util"
	"github.com/rs/zerolog/log"
)

// Frigate 0.18 redacts saved credentials in /api/config responses: the mqtt
// password comes back as the sentinel "__FRIGATE_SAVED_CREDENTIAL__" (or the
// key is dropped when unset), never the real password. See
// frigate/util/config.py redact_credential and frigate/const.py
// REDACTED_CREDENTIAL_SENTINEL. A password is therefore only ever echoed
// back when Frigate itself returns it verbatim, which 0.18 does not do.
const frigateRedactedCredential = "__FRIGATE_SAVED_CREDENTIAL__"

type FrigateMetaCamera struct {
	Name  string   `json:"name"`
	Zones []string `json:"zones"`
}

type FrigateMetaMQTT struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

type FrigateMetaOutput struct {
	Body struct {
		Cameras   []FrigateMetaCamera `json:"cameras"`
		Zones     []string            `json:"zones"`
		Labels    []string            `json:"labels"`
		SubLabels []string            `json:"sublabels"`
		MQTT      *FrigateMetaMQTT    `json:"mqtt,omitempty"`
		Version   string              `json:"version,omitempty"`
	}
}

// frigateConfig is the subset of Frigate's /api/config response this
// endpoint needs. Frigate 0.18 returns cameras keyed by name, each with a
// "zones" map keyed by zone name; a global "objects" section with a "track"
// label list; and a "model" section whose "labelmap" holds label overrides.
type frigateConfig struct {
	Cameras map[string]struct {
		Zones map[string]json.RawMessage `json:"zones"`
	} `json:"cameras"`
	Objects struct {
		Track []string `json:"track"`
	} `json:"objects"`
	Model struct {
		Labelmap map[string]string `json:"labelmap"`
	} `json:"model"`
	MQTT struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
	} `json:"mqtt"`
}

// GetFrigateMeta returns cameras + zones, the union of all zones, tracked
// labels, observed sub-labels, MQTT settings and the Frigate version, all
// read live from the configured Frigate server. Every piece is independent:
// if Frigate is unreachable or a piece is missing or malformed, the rest is
// still returned with empty arrays / omitted fields for the missing pieces.
// The endpoint itself always answers 200.
func GetFrigateMeta(ctx context.Context, input *struct{}) (*FrigateMetaOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/frigate_meta").
		Str("method", "GET").
		Msg("Received API request")

	resp := &FrigateMetaOutput{}
	resp.Body.Cameras = []FrigateMetaCamera{}
	resp.Body.Zones = []string{}
	resp.Body.Labels = []string{}
	resp.Body.SubLabels = []string{}

	server := strings.TrimSuffix(strings.TrimSpace(config.ConfigData.Frigate.Server), "/")
	if server == "" {
		return resp, nil
	}
	insecure := config.ConfigData.Frigate.Insecure
	headers := config.ConfigData.Frigate.Headers

	// /api/config: cameras, zones, labels, MQTT.
	if body, err := util.HTTPGet(server+"/api/config", insecure, "", headers...); err == nil {
		var cfg frigateConfig
		if err := json.Unmarshal(body, &cfg); err == nil {
			zoneSet := map[string]bool{}
			names := make([]string, 0, len(cfg.Cameras))
			for name := range cfg.Cameras {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				zones := make([]string, 0, len(cfg.Cameras[name].Zones))
				for zone := range cfg.Cameras[name].Zones {
					zones = append(zones, zone)
					zoneSet[zone] = true
				}
				sort.Strings(zones)
				resp.Body.Cameras = append(resp.Body.Cameras, FrigateMetaCamera{Name: name, Zones: zones})
			}
			for zone := range zoneSet {
				resp.Body.Zones = append(resp.Body.Zones, zone)
			}
			sort.Strings(resp.Body.Zones)

			if len(cfg.Objects.Track) > 0 {
				resp.Body.Labels = append(resp.Body.Labels, cfg.Objects.Track...)
			} else if len(cfg.Model.Labelmap) > 0 {
				for _, label := range cfg.Model.Labelmap {
					resp.Body.Labels = append(resp.Body.Labels, label)
				}
				sort.Strings(resp.Body.Labels)
			}

			mqtt := &FrigateMetaMQTT{
				Enabled: cfg.MQTT.Enabled,
				Host:    cfg.MQTT.Host,
				Port:    cfg.MQTT.Port,
				User:    cfg.MQTT.User,
			}
			// Only echo a password Frigate returned verbatim. Frigate 0.18
			// redacts it to the sentinel (or drops the key), so in practice
			// this field is always omitted.
			if cfg.MQTT.Password != "" && cfg.MQTT.Password != frigateRedactedCredential {
				mqtt.Password = cfg.MQTT.Password
			}
			resp.Body.MQTT = mqtt
		} else {
			log.Warn().Err(err).Msg("Unable to parse Frigate /api/config response")
		}
	} else {
		log.Warn().Err(err).Msg("Unable to reach Frigate /api/config")
	}

	// /api/events: distinct non-empty sub_labels, capped at 50, sorted.
	if body, err := util.HTTPGet(server+"/api/events?limit=200&include_thumbnails=0", insecure, "", headers...); err == nil {
		var events []struct {
			SubLabel string `json:"sub_label"`
		}
		if err := json.Unmarshal(body, &events); err == nil {
			seen := map[string]bool{}
			for _, ev := range events {
				if ev.SubLabel != "" {
					seen[ev.SubLabel] = true
				}
			}
			for sub := range seen {
				resp.Body.SubLabels = append(resp.Body.SubLabels, sub)
			}
			sort.Strings(resp.Body.SubLabels)
			if len(resp.Body.SubLabels) > 50 {
				resp.Body.SubLabels = resp.Body.SubLabels[:50]
			}
		} else {
			log.Warn().Err(err).Msg("Unable to parse Frigate /api/events response")
		}
	} else {
		log.Warn().Err(err).Msg("Unable to reach Frigate /api/events")
	}

	// /api/version: plain-text version string, same call frigate_check uses.
	if body, err := util.HTTPGet(server+"/api/version", insecure, "", headers...); err == nil {
		resp.Body.Version = strings.TrimSpace(string(body))
	} else {
		log.Warn().Err(err).Msg("Unable to reach Frigate /api/version")
	}

	return resp, nil
}
