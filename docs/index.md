# Docs for Frigate Notify Plus

Hi there, you've found the documentation site for [izerok/frigate-notify-plus](https://github.com/izerok/frigate-notify-plus). Thanks for your interest in the project!

If you get value out of this tool, please let me know by leaving a star:  <a class="github-button" href="https://github.com/izerok/frigate-notify-plus" data-icon="octicon-star" data-show-count="true" aria-label="Star izerok/frigate-notify-plus on GitHub"></a>

## About the Project

Frigate Notify Plus is a fork of [0x2142/frigate-notify](https://github.com/0x2142/frigate-notify) — a simple app designed to send notifications from [Frigate](https://github.com/blakeblackshear/frigate) NVR to your favorite platforms. Intended to be used with standalone Frigate installations - Home Assistant not required, MQTT is optional but recommended.

This fork adds GenAI review descriptions in notifications, a built-in Web UI console (port 8000) for configuring everything in the browser, per-camera cooldowns, routing rules, threat-level priorities, per-camera mute, scheduled GenAI summaries, an hourly notification cap, and the Bark notification channel.

## Features

**Event Polling**

- MQTT
- Direct via Frigate API

**Notification Methods**

- Bark
- Discord
- Gotify
- Matrix
- Mattermost
- Ntfy
- Pushover
- Signal
- SMTP
- Telegram
- Webhook
- And many others via the [Apprise API](https://github.com/caronc/apprise-api)

**Other**

- Aliveness monitor via HTTP GET (for use with tools like [HealthChecks](https://github.com/healthchecks/healthchecks) or [Uptime Kuma](https://github.com/louislam/uptime-kuma))


!!! tip
    If you use this code & have any specific feature requests - please feel free to open an [issue](https://github.com/izerok/frigate-notify-plus/issues) with the details of what you would like to see added!

<script async defer src="https://buttons.github.io/buttons.js"></script>

<br/>
