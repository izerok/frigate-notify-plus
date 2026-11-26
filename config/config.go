package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	envconfig "github.com/izerok/frigate-notify-plus/config/providers/env"
	secretsconfig "github.com/izerok/frigate-notify-plus/config/providers/secrets"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util/proxyx"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog/log"
	yml "gopkg.in/yaml.v3"
)

type Config struct {
	App     models.App     `koanf:"app" yaml:"app" json:"app" required:"false"`
	Frigate models.Frigate `koanf:"frigate" yaml:"frigate" json:"frigate" required:"true"`
	Alerts  models.Alerts  `koanf:"alerts" yaml:"alerts" json:"alerts" required:"true"`
	Monitor models.Monitor `koanf:"monitor" yaml:"monitor" json:"monitor" required:"false"`
}

var ConfigData Config
var ConfigFile string

// DataDir is the persistent data directory (config.yml, logs/, mute.json).
// Empty means the legacy layout: ./config.yml and log/app.log next to the
// working directory. Set from the -d/--data-dir flag or FN_DATA_DIR.
var DataDir string
var k = koanf.New(".")

// ResolveDataDir returns the data directory: the -d/--data-dir flag wins,
// then the FN_DATA_DIR environment variable, otherwise "" (legacy layout).
func ResolveDataDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v, ok := os.LookupEnv("FN_DATA_DIR"); ok && v != "" {
		return v
	}
	return ""
}

// Load opens & attempts to parse configuration file, returning any validation
// errors. Callers decide how to act on an invalid config: startup aborts, while
// a runtime reload keeps the previously-running config.
func Load() []string {
	// Set config file location, in order: -c flag (already in ConfigFile),
	// FN_CONFIGFILE, $DataDir/config.yml, legacy ./config.yml.
	if ConfigFile == "" {
		var ok bool
		ConfigFile, ok = os.LookupEnv("FN_CONFIGFILE")
		if !ok {
			if DataDir != "" {
				ConfigFile = filepath.Join(DataDir, "config.yml")
			} else {
				ConfigFile = "./config.yml"
			}
		}
	}

	// Set config defaults
	k.Load(structs.Provider(DefaultConfig, "koanf"), nil)

	// Attempt to load config from file
	log.Debug().Msgf("Loading config from file: %v", ConfigFile)
	if err := k.Load(file.Provider(ConfigFile), yaml.Parser()); err != nil {
		log.Warn().Err(err).Msg("Unable to load config from file")
	}

	// Attempt to load config from env var
	log.Debug().Msg("Checking for environment variables")
	if err := k.Load(envconfig.ProviderWithValue("FN_", "__", processENV), json.Parser()); err != nil {
		log.Debug().
			Err(err).
			Msg("Unable to load environment variables")
	}

	// Attempt to load config from docker secrets
	log.Debug().Msg("Checking for docker secrets")
	if err := k.Load(secretsconfig.ProviderWithValue("FN_", "__", processENV), json.Parser()); err != nil {
		log.Debug().
			Err(err).
			Msg("Unable to load docker secrets")
	}

	k.Unmarshal("", &ConfigData)

	log.Info().Msg("Config loaded")

	log.Trace().
		Interface("config", ConfigData).
		Msg("Config loaded")

	// Send config file to validation before completing
	validationErrors := ConfigData.Validate()

	log.Trace().
		Interface("config", ConfigData).
		Msg("Config file loaded & validation completed")

	if len(validationErrors) > 0 {
		fmt.Println()
		log.Error().Msg("Config validation failed:")
		for _, msg := range validationErrors {
			log.Error().Msgf(" - %v", msg)
		}
		fmt.Println()
	} else {
		log.Info().Msg("Config file validated!")
		ApplyProxyRuntime()
	}

	return validationErrors
}

// ApplyProxyRuntime publishes the proxy settings of ConfigData — the config
// the app is actually running — to the proxyx package, which notification
// senders consult on every delivery. It is deliberately called when a config
// is accepted (startup load, successful reload), never from Validate: that
// also runs on candidate configs that may still be rejected, and a rejected
// save must not change how the running app delivers notifications.
func ApplyProxyRuntime() {
	proxyx.Configure(ConfigData.Alerts.General.Proxy.URL, ConfigData.Alerts.General.Proxy.NoProxy, ConfigData.Frigate.Server)
}

func Save(skipBackup bool) {
	log.Debug().Msg("Writing new config file")

	data, err := yml.Marshal(&ConfigData)
	if err != nil {
		log.Error().Err(err).Msg("Unable to save config")
		return
	}

	// Store backup of original config, if requested
	if !skipBackup {
		original, err := os.Open(ConfigFile)
		if err != nil {
			log.Error().Err(err).Msg("Unable to create config backup")
		}
		defer original.Close()

		newFile := fmt.Sprintf("%s-%s.bak", ConfigFile, time.Now().Format("20060102150405"))
		copy, err := os.Create(newFile)
		if err != nil {
			log.Error().Err(err).Msg("Unable to create config backup")
		}
		defer copy.Close()

		io.Copy(copy, original)
		log.Info().Msgf("Created config file backup: %v", newFile)

	}

	err = os.WriteFile(ConfigFile, data, 0644)
	if err != nil {
		log.Error().Err(err).Msg("Unable to save config")
		return
	}

	log.Info().Msg("Config file saved")
}

func processENV(s, v string) (string, interface{}) {
	key := strings.ToLower(strings.TrimPrefix(s, "FN_"))

	// Split multiple values separated by semicolon into slice
	// for example FN_FRIGATE__CAMERAS__EXCLUDE="camera1;camera2"
	if strings.Contains(v, ";") {
		if strings.Contains(strings.ToLower(key), "headers") {
			var headers []map[string]string
			for _, header := range strings.Split(v, ";") {
				split := strings.Split(header, ":")
				if len(split) != 2 {
					continue
				}
				newHeader := map[string]string{strings.TrimSpace(split[0]): strings.TrimSpace(split[1])}
				headers = append(headers, newHeader)
			}
			return key, headers
		}
		return key, strings.Split(v, ";")
	}

	// Ensure that single header items also get mapped correctly
	if strings.Contains(strings.ToLower(key), "headers") {
		var headers []map[string]string
		split := strings.Split(v, ":")
		newHeader := map[string]string{strings.TrimSpace(split[0]): strings.TrimSpace(split[1])}
		headers = append(headers, newHeader)
		return key, headers
	}

	return key, v
}
