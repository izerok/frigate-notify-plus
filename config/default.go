package config

import "github.com/izerok/frigate-notify-plus/models"

// Default config values
var DefaultConfig Config = Config{
	App: models.App{
		Mode: "reviews",
		API: models.API{
			// Enabled by default in this fork: the Web UI console shares
			// this port, so it must be reachable on first run. LAN-only,
			// no authentication by design; do not expose the port publicly.
			Enabled: true,
			Port:    8000},
		Internal: models.Internal{
			HTTP: models.HTTP{
				Timeout:  10,
				Attempts: 6,
			},
		},
	},
	Frigate: models.Frigate{
		Server:    "",
		Insecure:  false,
		PublicURL: "",
		Headers:   nil,
		StartupCheck: models.StartupCheck{
			Attempts: 5,
			Interval: 30,
		},
		WebAPI: models.WebAPI{
			Enabled:  false,
			Interval: 30,
			TestMode: false,
		},
		MQTT: models.MQTT{
			Enabled:     false,
			Server:      "",
			Port:        1883,
			ClientID:    "frigate-notify-plus",
			Username:    "",
			Password:    "",
			TopicPrefix: "frigate",
		},
		Cameras: models.Cameras{
			Exclude: nil,
		},
	},
	Alerts: models.Alerts{
		General: models.General{
			Cooldown:         0,
			CameraCooldown:   nil,
			Title:            "Frigate Alert",
			TimeFormat:       "",
			NoSnap:           "allow",
			SnapBbox:         false,
			SnapTimestamp:    false,
			SnapCrop:         false,
			MaxSnapRetry:     10,
			NotifyOnce:       false,
			NotifyDetections: false,
			RecheckDelay:     0,
			AudioOnly:        "allow",
			GenAI: models.GenAIConfig{
				Enabled:         true,
				UpdateNotif:     true,
				SummaryIdleTime: 300,
			},
			ThreatPriority: models.ThreatPriority{
				NtfyNormal:               3,
				NtfySuspicious:           4,
				NtfyCritical:             5,
				GotifyNormal:             5,
				GotifySuspicious:         8,
				GotifyCritical:           10,
				PushoverNormal:           0,
				PushoverSuspicious:       1,
				PushoverCritical:         2,
				TelegramSilentNormal:     true,
				TelegramSilentSuspicious: false,
				TelegramSilentCritical:   false,
			},
		},
		Quiet: models.Quiet{
			Start: "",
			End:   "",
		},
		Zones: models.Zones{
			Unzoned: "allow",
			Allow:   nil,
			Block:   nil,
		},
		Labels: models.Labels{
			MinScore: 0,
			Allow:    nil,
			Block:    nil,
		},
		SubLabels: models.Labels{
			MinScore: 0,
			Allow:    nil,
			Block:    nil,
		},
		LicensePlate: models.LicensePlate{
			Enabled: false,
			Allow:   nil,
			Block:   nil,
		},
	},
	Monitor: models.Monitor{
		Enabled:  false,
		URL:      "",
		Interval: 60,
		Insecure: false,
	},
}
