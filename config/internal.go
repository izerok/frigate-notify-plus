package config

import "github.com/izerok/frigate-notify-plus/models"

var Internal models.InternalConfig

func init() {
	Internal.Status.Notifications.Enabled = true
	Internal.AppVersion = "v0.1.2"
	Internal.Status.Health = "n/a"
	Internal.Status.API = "n/a"
	Internal.Status.Frigate.API = "n/a"
	Internal.Status.Frigate.MQTT = "n/a"
	Internal.Status.Monitor = "n/a"
}
