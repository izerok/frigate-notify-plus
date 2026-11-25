package main

import (
	"embed"
	"flag"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	debuginfo "runtime/debug"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/izerok/frigate-notify-plus/api"
	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/events"
	"github.com/izerok/frigate-notify-plus/notifier"
	"github.com/izerok/frigate-notify-plus/util"
)

var debug, debugenv bool
var jsonlog, jsonlogenv bool
var nocolor, nocolorenv bool
var configFile string
var dataDirFlag string
var logLevel string

//go:embed templates/*
var NotifTemplates embed.FS

//go:embed webui/*
var WebUI embed.FS

func main() {
	config.Internal.Status.Health = "starting"

	// Parse flags
	flag.StringVar(&configFile, "c", "", "Configuration file location (default \"$DATA_DIR/config.yml\" when a data directory is set, else \"./config.yml\")")
	flag.StringVar(&dataDirFlag, "d", "", "Data directory holding config.yml and logs/ (default: none, legacy ./config.yml + log/app.log)")
	flag.StringVar(&dataDirFlag, "data-dir", "", "Same as -d")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging (Overrides loglevel, if also set)")
	flag.StringVar(&logLevel, "loglevel", "", "Set logging level")
	flag.BoolVar(&jsonlog, "jsonlog", false, "Enable JSON logging")
	flag.BoolVar(&nocolor, "nocolor", false, "Disable color on console logging")
	flag.Parse()

	// Resolve the data directory before logging is set up: it decides
	// where the log file lives.
	config.DataDir = config.ResolveDataDir(dataDirFlag)
	config.Internal.Status.DataDir = config.DataDir

	// Set up logging: console plus a rotating file. The file lives in
	// $DATA_DIR/logs/ when a data directory is set, else legacy log/app.log.
	// If the data directory cannot be created or written, fall back to
	// console-only and warn — a bad log path must never fail startup.
	var logwriter io.Writer = os.Stdout
	logPath := "log/app.log"
	var logProblem error
	if config.DataDir != "" {
		logPath = filepath.Join(config.DataDir, "logs", "frigate-notify.log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			logProblem = err
			logPath = ""
		} else if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			logProblem = err
			logPath = ""
		} else {
			f.Close()
		}
	}

	_, jsonlogenv = os.LookupEnv("FN_JSONLOG")
	if !jsonlog && !jsonlogenv {
		_, nocolorenv = os.LookupEnv("FN_NOCOLOR")
		logwriter = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006/01/02 15:04:05 -0700", NoColor: nocolorenv || nocolor}
	}

	zerolog.TimeFieldFormat = "2006/01/02 15:04:05 -0700"
	writers := []io.Writer{logwriter}
	if logPath != "" {
		writers = append(writers, &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    10,
			MaxBackups: 5,
			LocalTime:  true,
		})
	}
	log.Logger = zerolog.New(zerolog.MultiLevelWriter(writers...)).With().Timestamp().Logger()
	if logProblem != nil {
		log.Warn().Err(logProblem).Msgf("Data directory %q is not writable - logging to console only", config.DataDir)
	} else if config.DataDir != "" {
		log.Info().Msgf("Data directory: %v", config.DataDir)
	}

	// Apply custom log level, if set
	if logLevel == "" {
		logLevel, _ = os.LookupEnv("FN_LOGLEVEL")
	}
	switch strings.ToLower(logLevel) {
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "debug":
		log.Debug().Msg("Debug logging enabled")
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.With().Caller().Logger()
	case "trace":
		log.Trace().Msg("Trace logging enabled")
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
		log.Logger = log.With().Caller().Logger()
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Enable debug logging if set
	_, debugenv = os.LookupEnv("FN_DEBUG")
	if debug || debugenv {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		// Add calling module to debug logs
		log.Logger = log.With().Caller().Logger()
		log.Debug().Msg("Debug logging enabled")
	}

	info, _ := debuginfo.ReadBuildInfo()
	buildinfo := make(map[string]interface{})
	for _, s := range info.Settings {
		buildinfo[s.Key] = s.Value
	}
	log.Info().Msgf("Frigate Notify Plus - %v", config.Internal.AppVersion)
	log.Trace().Fields(buildinfo).Msg("Build Info")
	log.Info().Msg("Starting...")

	// Load & validate config
	config.ConfigFile = configFile
	errs := config.Load()
	log.Info().Msgf("Using config file: %v", config.ConfigFile)

	// Load per-camera mutes (mute.json next to the config file). Missing or
	// corrupt files are not fatal; mutes are irrelevant in setup mode but
	// the call must not fail there, and ConfigFile is set by now.
	notifier.LoadMutes()

	setupMode := false
	if len(errs) > 0 {
		// First run without a config file: start in setup mode instead of
		// exiting, so the Web UI can collect the configuration. A config
		// file that exists but is invalid still aborts, as before.
		if _, statErr := os.Stat(config.ConfigFile); os.IsNotExist(statErr) {
			setupMode = true
			config.Internal.Status.Health = "setup"
			log.Warn().Msg("No config file found - starting in setup mode. Open the Web UI to configure Frigate-Notify-Plus.")
		} else {
			log.Fatal().Msg("Please fix config errors before restarting app.")
		}
	}

	// Message templates: on-disk files edited through the Web UI win over
	// the embedded copies. When a data directory is set, template edits live
	// in $DATA_DIR/templates/ so they survive container recreation; without
	// one, the legacy "templates" directory next to the working directory
	// is used. This must be set before TemplatesWithDiskFallback, which
	// captures the directory at construction time.
	if config.DataDir != "" {
		notifier.TemplateDir = filepath.Join(config.DataDir, "templates")
	}
	notifier.TemplateFiles = notifier.TemplatesWithDiskFallback(NotifTemplates)
	log.Info().Msgf("Using template directory: %v", notifier.TemplateDir)
	// The raw embedded FS, so the Web UI can show the factory defaults.
	notifier.EmbeddedTemplates = NotifTemplates

	// Notification history: append to $DATA_DIR/history.jsonl so the Web UI
	// history survives container recreation; without a data directory the
	// history stays in memory only. Wired before the scheduler and the API
	// server start, so no entry can be recorded before persistence is on.
	if config.DataDir != "" {
		historyPath := filepath.Join(config.DataDir, "history.jsonl")
		notifier.SetHistoryFile(historyPath)
		log.Info().Msgf("Using history file: %v", historyPath)
	}

	// Set up monitor
	if config.ConfigData.Monitor.Enabled {
		log.Debug().Msg("App monitoring enabled.")
		go func() {
			for {
				_, err := util.HTTPGet(config.ConfigData.Monitor.URL, config.ConfigData.Monitor.Insecure, "")
				if err != nil {
					config.Internal.Status.Monitor = err.Error()
					log.Warn().
						Err(err).
						Msg("Unable to reach polling monitoring URL")
				}
				config.Internal.Status.Monitor = "ok"
				log.Debug().Msg("Completed monitoring check-in.")
				time.Sleep(time.Duration(config.ConfigData.Monitor.Interval) * time.Second)
			}
		}()
	}

	// Set up event cache
	events.InitZoneCache()
	defer events.CloseZoneCache()

	// Start the scheduled GenAI summary ticker (in-process, one-minute
	// ticks; it reads the live config, so a PUT takes effect without a
	// restart). Runs in both WebAPI and MQTT modes.
	notifier.StartGenAIScheduler()

	// Start API server if enabled (always on in setup mode, otherwise the
	// Web UI would be unreachable on first run)
	if setupMode {
		config.ConfigData.App.API.Enabled = true
	}
	if config.ConfigData.App.API.Enabled {
		// The embed keeps the "webui/" prefix; strip it so the router can
		// read "index.html" at the root of the served files.
		webuiFS, subErr := fs.Sub(WebUI, "webui")
		if subErr != nil {
			log.Error().Err(subErr).Msg("Failed to load embedded Web UI")
			webuiFS = nil
		}
		err := api.RunAPIServer(webuiFS)
		if err != nil {
			config.Internal.Status.API = err.Error()
			log.Error().Err(err).Msg("Failed to start API server")
		} else {
			config.Internal.Status.API = "ok"
			log.Info().Msgf("API server ready on :%v", config.ConfigData.App.API.Port)
		}
	}

	// In setup mode there is no event source configured yet: serve the
	// API + Web UI and wait until the user finishes the setup (or stops us).
	if setupMode {
		log.Info().Msg("Setup mode: waiting for configuration via Web UI...")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		return
	}

	// Loop & watch for events
	if config.ConfigData.Frigate.WebAPI.Enabled {
		log.Info().Msg("App ready!")
		config.Internal.Status.Health = "ok"
		for {
			events.QueryAPI()
			time.Sleep(time.Duration(config.ConfigData.Frigate.WebAPI.Interval) * time.Second)
		}
	}

	// Connect MQTT
	if config.ConfigData.Frigate.MQTT.Enabled {
		log.Debug().Msg("Connecting to MQTT Server...")
		events.SubscribeMQTT()
		defer events.DisconnectMQTT()
		log.Info().Msg("App ready!")
		config.Internal.Status.Health = "ok"
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
	}

}
