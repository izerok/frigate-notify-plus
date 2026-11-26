# Changelog

## [v0.1.0](https://github.com/izerok/frigate-notify-plus/releases/tag/v0.1.0) - Nov 25 2026
 - First release on this fork's own version line: Frigate Notify Plus no longer follows upstream frigate-notify versioning — the `v0.5.4-webui.*` prerelease series ends at `v0.5.4-webui.15`, and the app now reports itself as `v0.1.0`
 - Redesign the Web UI console: sidebar shell with a resident status summary, hash-based deep links (`#/status`, `#/config/<section>`, ...), a status dashboard, a floating save dock with change count and save-diff preview, and a tabbed template editor
 - New default Web UI palette: a brighter blue design system replaces the previous light theme; dark, eye-care green and Argon themes remain available
 - Web UI is now bilingual: a 中文 / English switcher in the sidebar switches all UI text in place (choice stored in the browser, Chinese by default)
 - Fix the status page notification tile briefly reporting "on" before the real notification state has loaded; it now shows a loading state until the state is known
 - Fix the GenAI and threat-priority config cards' "modified" badges lighting up for the wrong section, and scope those cards' restore-defaults buttons to their own settings
 - Slim down the bilingual README (347 -> 90 lines) and add Web UI screenshots
 - Rename the project to Frigate Notify Plus (`github.com/izerok/frigate-notify-plus`, binary/image `frigate-notify-plus`); Web UI console, GenAI notifications, per-camera cooldown, routing rules, threat priorities, per-camera mute, Bark channel, GenAI escalation, hourly notification cap, and scheduled GenAI summaries are fork features — see the [README](https://github.com/izerok/frigate-notify-plus/blob/main/README.md)
 - Add ability to send the snapshot first, then replace it in-place with the event video clip via [Telegram](config/file.md#telegram) (`send_clip_after_snapshot`), with a configurable `clip_max_size` upload limit
 - Add option to re-encode [Telegram](config/file.md#telegram) clips to a smaller video with ffmpeg before upload (`clip_transcode`), optionally on a GPU via VAAPI. The Docker image is now based on Alpine & includes ffmpeg
 - Frigate 0.18 support: threat level now renders as "Normal", "Suspicious" or "Critical" to match Frigate's 0-2 scale (was "Minor"/"Moderate"/"High")
 - Add `.Extra.GenAIObservations` template variable (Frigate 0.18+) and append it as a timeline to Telegram GenAI updates; also included in the default webhook payload
 - Include Frigate's error message when a GenAI activity summary request is rejected
 - Fix GenAI activity summaries being sent with Frigate's "No activity/concerns were found" placeholder text; a summary is now only requested when a review in the period was flagged by GenAI
 - Fix GenAI update notifications being sent for reviews whose initial alert was dropped (e.g. detection-only reviews with `notify_detections` off, or filtered zones/labels/cameras)
 - Fix duplicate Telegram alerts, and GenAI captions being undone by the clip swap, when a GenAI update arrived while the initial alert was still being sent; the update now waits for that alert to finish and then edits it
 - In `reviews` mode, a tracked object now alerts only once per zone even when it appears in several Frigate reviews, until it has gone 1 hour without alerting on a new zone (previously each new review could alert again)
 - In `reviews` mode, an object that already alerted (on its zone, or at all with `notify_once`) no longer stops a new object in the same review from alerting; the alert is sent for the new objects
 - In `reviews` mode, an object dropped by a filter (e.g. quiet hours or `min_score`) can still alert once it passes; previously its zone already counted as notified
