package models

import yaml "gopkg.in/yaml.v3"

type App struct {
	Mode     string   `koanf:"mode" yaml:"mode" json:"mode" enum:"events,reviews" doc:"Type of polling method used when connecting to Frigate" default:"reviews"`
	API      API      `koanf:"api" yaml:"api" json:"api" doc:"Frigate-Notify-Plus API settings"`
	Internal Internal `koanf:"internal" yaml:"internal,omitempty" json:"internal,omitempty" hidden:"true" doc:"Internal settings that alter the behavior of Frigate-Notify-Plus"`
}

type API struct {
	Enabled bool `koanf:"enabled" yaml:"enabled" json:"enabled" doc:"Enable Frigate-Notify-Plus API server" enum:"true,false" default:"false"`
	Port    int  `koanf:"port" yaml:"port,omitempty" json:"port,omitempty" doc:"API server port" minimum:"1" maximum:"65535" default:"8000"`
}

type Internal struct {
	HTTP HTTP `koanf:"http" yaml:"http,omitempty" json:"http,omitempty" doc:"Frigate-Notify-Plus outbound HTTP settings"`
}

type HTTP struct {
	Timeout  int `koanf:"timeout" yaml:"timeout,omitempty" json:"timeout,omitempty" doc:"Outbound HTTP timeout" default:"10"`
	Attempts int `koanf:"attempts" yaml:"attempts,omitempty" json:"attempts,omitempty" doc:"Max attempts trying to connect to HTTP endpoint" default:"6"`
}

type Frigate struct {
	Server       string              `koanf:"server" yaml:"server" json:"server" validate:"required" example:"http://192.0.2.10:5000" doc:"Server hostname, IP address, or URL for Frigate"`
	Insecure     bool                `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" enum:"true,false" doc:"Ignore TLS/SSL errors" default:"false"`
	PublicURL    string              `koanf:"public_url" yaml:"public_url,omitempty" json:"public_url,omitempty" example:"https://frigate.test" doc:"Public/External-reachable URL for Frigate" default:""`
	Username     string              `koanf:"username" yaml:"username,omitempty" json:"username,omitempty" doc:"Frigate username" default:""`
	Password     string              `koanf:"password" yaml:"password,omitempty" json:"password,omitempty" doc:"Frigate password" default:""`
	Headers      []map[string]string `koanf:"headers" yaml:"headers,omitempty" json:"headers,omitempty" doc:"HTTP headers to include with requests to Frigate"`
	StartupCheck StartupCheck        `koanf:"startup_check" yaml:"startup_check,omitempty" json:"startup_check,omitempty" doc:"Check connectivity to Frigate at app startup"`
	WebAPI       WebAPI              `koanf:"webapi" yaml:"webapi,omitempty" json:"webapi,omitempty" doc:"Event collection via Frigate API"`
	MQTT         MQTT                `koanf:"mqtt" yaml:"mqtt,omitempty" json:"mqtt,omitempty" doc:"Event collection via MQTT"`
	Cameras      Cameras             `koanf:"cameras" yaml:"cameras,omitempty" json:"cameras,omitempty" doc:"Camera settings"`
}

type StartupCheck struct {
	Attempts int `koanf:"attempts" yaml:"attempts,omitempty" json:"attempts,omitempty" doc:"Maximum attempts for connecting to Frigate" minimum:"1" maximum:"10000000" default:"5"`
	Interval int `koanf:"interval" yaml:"interval,omitempty" json:"interval,omitempty" doc:"Interval for connection attempts" minimum:"1" maximum:"10000000" default:"30"`
}

type WebAPI struct {
	Enabled  bool `koanf:"enabled" yaml:"enabled" json:"enabled" enum:"true,false" doc:"Enable event collection via Frigate API" default:"false"`
	Interval int  `koanf:"interval" yaml:"interval,omitempty" json:"interval,omitempty" doc:"Interval of API event collection from Frigate" minimum:"1" maximum:"65535" default:"30"`
	TestMode bool `koanf:"testmode" yaml:"testmode,omitempty" json:"testmode,omitempty" enum:"true,false" doc:"Used for testing only" hidden:"true" default:"false"`
}

type MQTT struct {
	Enabled     bool   `koanf:"enabled" yaml:"enabled" json:"enabled" enum:"true,false" doc:"Enable event collection via MQTT" default:"false"`
	Server      string `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"MQTT server address" default:""`
	Port        int    `koanf:"port" yaml:"port,omitempty" json:"port,omitempty" doc:"MQTT port" minimum:"1" maximum:"65535" default:"1883"`
	ClientID    string `koanf:"clientid" yaml:"clientid,omitempty" json:"clientid,omitempty" doc:"MQTT client ID" default:"frigate-notify-plus"`
	Username    string `koanf:"username" yaml:"username,omitempty" json:"username,omitempty" doc:"MQTT username" default:""`
	Password    string `koanf:"password" yaml:"password,omitempty" json:"password,omitempty" doc:"MQTT password" default:""`
	TopicPrefix string `koanf:"topic_prefix" yaml:"topic_prefix,omitempty" json:"topic_prefix,omitempty" doc:"MQTT topic prefix" default:"frigate"`
}

type Cameras struct {
	Exclude []string `koanf:"exclude" yaml:"exclude" json:"exclude" uniqueItems:"true" doc:"Exclude cameras from alerting"`
}

type Alerts struct {
	General      General      `koanf:"general" yaml:"general,omitempty" json:"general,omitempty" doc:"Common alert settings"`
	Quiet        Quiet        `koanf:"quiet" yaml:"quiet,omitempty" json:"quiet,omitempty" doc:"Alert quiet periods"`
	Zones        Zones        `koanf:"zones" yaml:"zones,omitempty" json:"zones,omitempty" doc:"Allow/Block zones from alerting"`
	Labels       Labels       `koanf:"labels" yaml:"labels,omitempty" json:"labels,omitempty" doc:"Allow/Block labels from alerting"`
	SubLabels    Labels       `koanf:"sublabels" yaml:"sublabels,omitempty" json:"sublabels,omitempty" doc:"Allow/Block sublabels from alerting"`
	LicensePlate LicensePlate `koanf:"license_plate" yaml:"license_plate,omitempty" json:"license_plate,omitempty" doc:"License plate recognition settings"`
	AppriseAPI   []AppriseAPI `koanf:"apprise_api" yaml:"apprise_api,omitempty" json:"apprise_api,omitempty" doc:"Apprise API notification settings"`
	Bark         []Bark       `koanf:"bark" yaml:"bark,omitempty" json:"bark,omitempty" doc:"Bark notification settings (iOS push via bark.day or a self-hosted server)"`
	Discord      []Discord    `koanf:"discord" yaml:"discord,omitempty" json:"discord,omitempty" doc:"Discord notification settings"`
	Gotify       []Gotify     `koanf:"gotify" yaml:"gotify,omitempty" json:"gotify,omitempty" doc:"Gotify notification settings"`
	Matrix       []Matrix     `koanf:"matrix" yaml:"matrix,omitempty" json:"matrix,omitempty" doc:"Matrix notification settings"`
	Mattermost   []Mattermost `koanf:"mattermost" yaml:"mattermost,omitempty" json:"mattermost,omitempty" doc:"Mattermost notification settings"`
	Ntfy         []Ntfy       `koanf:"ntfy" yaml:"ntfy,omitempty" json:"ntfy,omitempty" doc:"Ntfy notification settings"`
	Pushover     []Pushover   `koanf:"pushover" yaml:"pushover,omitempty" json:"pushover,omitempty" doc:"Pushover notification settings"`
	Signal       []Signal     `koanf:"signal" yaml:"signal,omitempty" json:"signal,omitempty" doc:"Signal notification settings"`
	SMTP         []SMTP       `koanf:"smtp" yaml:"smtp,omitempty" json:"smtp,omitempty" doc:"SMTP notification settings"`
	Telegram     []Telegram   `koanf:"telegram" yaml:"telegram,omitempty" json:"telegram,omitempty" doc:"Telegram notification settings"`
	Webhook      []Webhook    `koanf:"webhook" yaml:"webhook,omitempty" json:"webhook,omitempty" doc:"Webhook notification settings"`
	Routing      Routing      `koanf:"routing" yaml:"routing,omitempty" json:"routing,omitempty" doc:"Routing rules that narrow an alert to selected providers; with no rules, or no matching rule, every eligible provider receives the alert"`
}

// Routing holds the ordered routing rules evaluated for each alert. Rules
// are tried in order; the targets of every matching rule are combined.
type Routing struct {
	Rules      []RoutingRule    `koanf:"rules" yaml:"rules,omitempty" json:"rules,omitempty" doc:"Routing rules, evaluated in order"`
	Escalation []EscalationRule `koanf:"escalation" yaml:"escalation,omitempty" json:"escalation,omitempty" doc:"GenAI escalation rules: when a GenAI update reaches MinThreat, a fresh alert is sent to the rule targets in addition to the caption edit on the originally notified channels"`
}

// EscalationRule sends a FRESH alert when a GenAI update for an already
// notified review reaches MinThreat. The initial alert carries threat 0 (no
// GenAI data yet), so a "critical goes everywhere" rule cannot fire on the
// initial alert; escalation closes that gap. Each (review, rule) pair fires
// at most once, but a later update with a still-higher threat can fire a
// higher rule.
type EscalationRule struct {
	MinThreat *int            `koanf:"min_threat" yaml:"min_threat,omitempty" json:"min_threat,omitempty" doc:"Minimum threat level (0 Normal, 1 Suspicious, 2 Critical) a GenAI update must reach to escalate" default:"1"`
	Targets   []RoutingTarget `koanf:"targets" yaml:"targets,omitempty" json:"targets,omitempty" doc:"Provider profiles that receive the fresh escalation alert"`
}

// RoutingRule narrows an alert to selected provider profiles. Every
// non-empty condition list must match (OR within a list); an empty list
// matches everything.
type RoutingRule struct {
	Name      string          `koanf:"name" yaml:"name,omitempty" json:"name,omitempty" doc:"Rule name, shown in validation errors" default:""`
	Cameras   []string        `koanf:"cameras" yaml:"cameras,omitempty" json:"cameras,omitempty" doc:"Cameras this rule applies to; empty matches all cameras"`
	Labels    []string        `koanf:"labels" yaml:"labels,omitempty" json:"labels,omitempty" doc:"Labels this rule applies to; empty matches all labels"`
	Zones     []string        `koanf:"zones" yaml:"zones,omitempty" json:"zones,omitempty" doc:"Zones this rule applies to; empty matches all zones"`
	Sublabels []string        `koanf:"sublabels" yaml:"sublabels,omitempty" json:"sublabels,omitempty" doc:"Sublabels this rule applies to; empty matches all sublabels"`
	MinThreat *int            `koanf:"min_threat" yaml:"min_threat,omitempty" json:"min_threat,omitempty" doc:"Minimum threat level (0 Normal, 1 Suspicious, 2 Critical) an event must reach to match this rule; -1 matches any threat level" default:"-1"`
	Targets   []RoutingTarget `koanf:"targets" yaml:"targets,omitempty" json:"targets,omitempty" doc:"Provider profiles that receive alerts matching this rule"`
}

// RoutingTarget selects provider profiles by name and index. Index -1
// selects every profile of that provider.
type RoutingTarget struct {
	Provider string `koanf:"provider" yaml:"provider,omitempty" json:"provider,omitempty" doc:"Provider key, e.g. webhook, telegram, ntfy" default:""`
	Index    *int   `koanf:"index" yaml:"index,omitempty" json:"index,omitempty" doc:"Profile index within the provider (0-based); -1 selects all profiles of the provider" default:"-1"`
}

type General struct {
	Cooldown                int            `koanf:"cooldown" json:"cooldown,omitempty" yaml:"cooldown" minimum:"0" default:"0" doc:"Seconds to wait between notifications from any camera; 0 disables the global cooldown"`
	MaxNotificationsPerHour int            `koanf:"max_notifications_per_hour" yaml:"max_notifications_per_hour,omitempty" json:"max_notifications_per_hour,omitempty" doc:"Maximum dispatched alerts per rolling hour across all providers (0 = unlimited); filtered, muted and cooled-down alerts do not count, test alerts are exempt, GenAI updates and escalations do count" default:"0"`
	CameraCooldown          map[string]int `koanf:"camera_cooldown" json:"camera_cooldown,omitempty" yaml:"camera_cooldown,omitempty" doc:"Per-camera notification cooldowns in seconds; omitted cameras have no camera-specific cooldown"`
	Title                   string         `koanf:"title" yaml:"title,omitempty" json:"title,omitempty" doc:"Notification title" default:"Frigate Alert"`
	TimeFormat              string         `koanf:"timeformat" yaml:"timeformat,omitempty" json:"timeformat,omitempty" doc:"Time format used in notifications" default:""`
	NoSnap                  string         `koanf:"nosnap,omitempty" yaml:"nosnap,omitempty" json:"nosnap" enum:"allow,drop" doc:"Allow/Drop events if they do not have a snapshot" default:"allow"`
	SnapBbox                bool           `koanf:"snap_bbox,omitempty" yaml:"snap_bbox,omitempty" json:"snap_bbox" enum:"true,false" doc:"Include bounding box on snapshots" default:"false"`
	SnapTimestamp           bool           `koanf:"snap_timestamp,omitempty" yaml:"snap_timestamp,omitempty" json:"snap_timestamp" enum:"true,false" doc:"Include timestamp on snapshots" default:"false"`
	SnapCrop                bool           `koanf:"snap_crop,omitempty" yaml:"snap_crop,omitempty" json:"snap_crop" enum:"true,false" doc:"Crop snapshots" default:"false"`
	SnapHiRes               bool           `koanf:"snap_hires" yaml:"snap_hires,omitempty" json:"snap_hires,omitempty" enum:"true,false" doc:"Collect snapshot from camera recording stream" default:"false"`
	MaxSnapRetry            int            `koanf:"max_snap_retry,omitempty" yaml:"max_snap_retry,omitempty" json:"max_snap_retry" doc:"Maximum number of retry attempts when snapshot is not ready yet" default:"10"`
	NotifyOnce              bool           `koanf:"notify_once,omitempty" yaml:"notify_once,omitempty" json:"notify_once" enum:"true,false" doc:"Only notify once per event (For app mode: events)" default:"false"`
	NotifyDetections        bool           `koanf:"notify_detections,omitempty" yaml:"notify_detections,omitempty" json:"notify_detections" enum:"true,false" doc:"Enable notifications on detection (For app mode: reviews)" default:"false"`
	RecheckDelay            int            `koanf:"recheck_delay" yaml:"recheck_delay,omitempty" json:"recheck_delay,omitempty" default:"0" doc:"Delay before re-checking event details from Frigate"`
	AudioOnly               string         `koanf:"audio_only" yaml:"audio_only,omitempty" json:"audio_only,omitempty" enum:"allow,drop" doc:"Allow/Drop events that only contain audio detections" default:"allow"`
	GenAI                   GenAIConfig    `koanf:"genai" yaml:"genai,omitempty" json:"genai,omitempty" doc:"GenAI feature settings"`
	ThreatPriority          ThreatPriority `koanf:"threat_priority" yaml:"threat_priority,omitempty" json:"threat_priority,omitempty" doc:"Per-threat-level notification priority per provider"`
}

// ThreatPriority maps the GenAI threat level of an event (Normal,
// Suspicious, Critical) to a per-provider notification priority. It only
// applies when an event carries GenAI data; events without GenAI data use
// threat level 0 (Normal), which equals each provider's default.
type ThreatPriority struct {
	NtfyNormal               int  `koanf:"ntfy_normal" yaml:"ntfy_normal,omitempty" json:"ntfy_normal,omitempty" doc:"Ntfy X-Priority for Normal threat level (1-5)" minimum:"1" maximum:"5" default:"3"`
	NtfySuspicious           int  `koanf:"ntfy_suspicious" yaml:"ntfy_suspicious,omitempty" json:"ntfy_suspicious,omitempty" doc:"Ntfy X-Priority for Suspicious threat level (1-5)" minimum:"1" maximum:"5" default:"4"`
	NtfyCritical             int  `koanf:"ntfy_critical" yaml:"ntfy_critical,omitempty" json:"ntfy_critical,omitempty" doc:"Ntfy X-Priority for Critical threat level (1-5)" minimum:"1" maximum:"5" default:"5"`
	GotifyNormal             int  `koanf:"gotify_normal" yaml:"gotify_normal,omitempty" json:"gotify_normal,omitempty" doc:"Gotify priority for Normal threat level (0-10)" minimum:"0" maximum:"10" default:"5"`
	GotifySuspicious         int  `koanf:"gotify_suspicious" yaml:"gotify_suspicious,omitempty" json:"gotify_suspicious,omitempty" doc:"Gotify priority for Suspicious threat level (0-10)" minimum:"0" maximum:"10" default:"8"`
	GotifyCritical           int  `koanf:"gotify_critical" yaml:"gotify_critical,omitempty" json:"gotify_critical,omitempty" doc:"Gotify priority for Critical threat level (0-10)" minimum:"0" maximum:"10" default:"10"`
	PushoverNormal           int  `koanf:"pushover_normal" yaml:"pushover_normal,omitempty" json:"pushover_normal,omitempty" doc:"Pushover priority for Normal threat level (-2-2)" minimum:"-2" maximum:"2" default:"0"`
	PushoverSuspicious       int  `koanf:"pushover_suspicious" yaml:"pushover_suspicious,omitempty" json:"pushover_suspicious,omitempty" doc:"Pushover priority for Suspicious threat level (-2-2)" minimum:"-2" maximum:"2" default:"1"`
	PushoverCritical         int  `koanf:"pushover_critical" yaml:"pushover_critical,omitempty" json:"pushover_critical,omitempty" doc:"Pushover priority for Critical threat level (-2-2)" minimum:"-2" maximum:"2" default:"2"`
	TelegramSilentNormal     bool `koanf:"telegram_silent_normal" yaml:"telegram_silent_normal,omitempty" json:"telegram_silent_normal,omitempty" doc:"Send Telegram alerts for Normal threat level without sound (disable_notification)" default:"true"`
	TelegramSilentSuspicious bool `koanf:"telegram_silent_suspicious" yaml:"telegram_silent_suspicious,omitempty" json:"telegram_silent_suspicious,omitempty" doc:"Send Telegram alerts for Suspicious threat level without sound (disable_notification)" default:"false"`
	TelegramSilentCritical   bool `koanf:"telegram_silent_critical" yaml:"telegram_silent_critical,omitempty" json:"telegram_silent_critical,omitempty" doc:"Send Telegram alerts for Critical threat level without sound (disable_notification)" default:"false"`
}

type GenAIConfig struct {
	Enabled         bool            `koanf:"enabled" yaml:"enabled" json:"enabled" enum:"true,false" doc:"Include GenAI data in notifications" default:"true"`
	UpdateNotif     bool            `koanf:"update_notif" yaml:"update_notif" json:"update_notif" enum:"true,false" doc:"Send updated notification when GenAI data arrives after initial alert" default:"true"`
	SummaryIdleTime int             `koanf:"summary_idle_time" yaml:"summary_idle_time,omitempty" json:"summary_idle_time,omitempty" doc:"Seconds of inactivity after notifications before requesting a GenAI summary from Frigate (0 to disable)" default:"300"`
	Schedules       []GenAISchedule `koanf:"schedules" yaml:"schedules,omitempty" json:"schedules,omitempty" doc:"Scheduled GenAI summaries (local time, at most once per schedule per day)"`
}

// GenAISchedule requests a GenAI summary from Frigate at a fixed local time
// and sends it as a notification. Window is "today" (midnight to run time)
// or "last_Nh" (N hours back from run time, e.g. last_11h). Targets empty
// means every enabled provider; otherwise only the listed profiles.
type GenAISchedule struct {
	Enabled *bool           `koanf:"enabled" yaml:"enabled,omitempty" json:"enabled,omitempty" doc:"Enable this scheduled summary" default:"true"`
	Time    string          `koanf:"time" yaml:"time,omitempty" json:"time,omitempty" doc:"Local time (HH:MM) to run the summary" example:"20:00"`
	Window  string          `koanf:"window" yaml:"window,omitempty" json:"window,omitempty" doc:"Time window to summarize: 'today' (midnight to run time) or 'last_Nh' (N hours back)" example:"today"`
	Prompt  string          `koanf:"prompt" yaml:"prompt,omitempty" json:"prompt,omitempty" doc:"Prompt sent to Frigate's GenAI chat for this schedule; empty uses the default 'what happened in this window' prompt"`
	Targets []RoutingTarget `koanf:"targets" yaml:"targets,omitempty" json:"targets,omitempty" doc:"Provider profiles that receive the scheduled summary; empty sends to every enabled provider"`
}

type LicensePlate struct {
	Enabled bool     `koanf:"enabled" yaml:"enabled,omitempty" json:"enabled,omitempty" enum:"true,false" doc:"Enable waiting for license plate recognition when car & license plate are detected" default:"false"`
	Allow   []string `koanf:"allow" yaml:"allow,omitempty" json:"allow,omitempty" doc:"List of license plates to allow alerts from"`
	Block   []string `koanf:"block" yaml:"block,omitempty" json:"block,omitempty" doc:"List of license plates to always block"`
}

type Quiet struct {
	Start string `koanf:"start" yaml:"start,omitempty" json:"start,omitempty" example:"02:30" pattern:"(\\d)?\\d:\\d\\d" doc:"Start time for quiet hours" default:""`
	End   string `koanf:"end" yaml:"end,omitempty" json:"end,omitempty" example:"05:45" pattern:"(\\d)?\\d:\\d\\d" doc:"End time for quiet hours" default:""`
}

type Zones struct {
	Unzoned string   `koanf:"unzoned" yaml:"unzoned,omitempty" json:"unzoned,omitempty" enum:"allow,drop" doc:"Allow/Drop events when object is outside a zone" default:"allow"`
	Allow   []string `koanf:"allow" yaml:"allow,omitempty" json:"allow,omitempty" doc:"List of zones to allow alerts from"`
	Block   []string `koanf:"block" yaml:"block,omitempty" json:"block,omitempty" doc:"List of zones to always block"`
}

type Labels struct {
	MinScore float64  `koanf:"min_score" yaml:"min_score" json:"min_score" minimum:"0" maximum:"100" doc:"Set minimum score before events will notify" default:"0"`
	Allow    []string `koanf:"allow" yaml:"allow,omitempty" json:"allow,omitempty" doc:"List of labels to allow alerts from"`
	Block    []string `koanf:"block" yaml:"block,omitempty" json:"block,omitempty" doc:"List of labels to always block"`
}

type AlertFilter struct {
	Cameras   []string           `koanf:"cameras" yaml:"cameras,omitempty" json:"cameras,omitempty" doc:"List of cameras that will use this alert provider"`
	Zones     []string           `koanf:"zones" yaml:"zones,omitempty" json:"zones,omitempty" doc:"List of zones that will use this alert provider"`
	Quiet     Quiet              `koanf:"quiet" yaml:"quiet,omitempty" json:"quiet,omitempty" doc:"Quiet period for this alert provider"`
	Labels    []string           `koanf:"labels" yaml:"labels,omitempty" json:"labels,omitempty" doc:"List of labels that will use this alert provider"`
	Sublabels []string           `koanf:"sublabels" yaml:"sublabels,omitempty" json:"sublabels,omitempty" doc:"List of sublabels that will use this alert provider"`
	MinScore  map[string]float64 `koanf:"min_score" yaml:"min_score,omitempty" json:"min_score,omitempty" doc:"Per-label minimum detection score (0.0-1.0); a label's detections are dropped only when every scored detection of that label is below its minimum; detections without a score are never dropped"`
}

type AlertCommon struct {
	Enabled  bool        `koanf:"enabled" yaml:"enabled" json:"enabled" enum:"true,false" doc:"Enable notifications via this provider" default:"false"`
	Title    string      `koanf:"title" yaml:"title,omitempty" json:"title,omitempty" doc:"Title for alerts from this provider"`
	Template string      `koanf:"template" yaml:"template,omitempty" json:"template,omitempty" doc:"Custom message template" default:""`
	Filters  AlertFilter `koanf:"filters" yaml:"filters,omitempty" json:"filters,omitempty" doc:"Filter notifications sent via this provider"`
}

type AppriseAPI struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string   `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Apprise API URL to send alerts" default:""`
	Token       string   `koanf:"token" yaml:"token,omitempty" json:"token,omitempty" doc:"Apprise API config token"`
	Tags        []string `koanf:"tags" yaml:"tags,omitempty" json:"tags,omitempty" doc:"Notification group tags to receive alert"`
	URLs        []string `koanf:"urls" yaml:"urls,omitempty" json:"urls,omitempty" doc:"Apprise notification URLs to send alerts to"`
	Insecure    bool     `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
}

// Bark sends iOS push notifications via a Bark server (bark.day). The
// device key travels in the request path (POST {server}/{device_key}) and is
// never part of the JSON body.
type Bark struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Bark server URL (default https://api.day.app)" default:"https://api.day.app"`
	DeviceKey   string `koanf:"device_key" yaml:"device_key,omitempty" json:"device_key,omitempty" doc:"Bark device key"`
	Sound       string `koanf:"sound" yaml:"sound,omitempty" json:"sound,omitempty" doc:"Bark notification sound (e.g. alarm.caf)"`
	Badge       int    `koanf:"badge" yaml:"badge,omitempty" json:"badge,omitempty" doc:"Bark badge number; 0 omits the badge field" default:"0"`
	Icon        string `koanf:"icon" yaml:"icon,omitempty" json:"icon,omitempty" doc:"Bark notification icon URL"`
	URL         string `koanf:"url" yaml:"url,omitempty" json:"url,omitempty" doc:"URL opened when the Bark notification is tapped"`
	Group       string `koanf:"group" yaml:"group,omitempty" json:"group,omitempty" doc:"Bark notification group"`
	Level       string `koanf:"level" yaml:"level,omitempty" json:"level,omitempty" enum:"active,timeSensitive,passive" doc:"Bark notification level (active, timeSensitive, passive); empty sends the server default"`
}

type Discord struct {
	AlertCommon  `koanf:",squash" yaml:",inline"`
	Webhook      string `koanf:"webhook" yaml:"webhook,omitempty" json:"webhook,omitempty" doc:"Discord webhook URL to send alerts" default:""`
	DisableEmbed bool   `koanf:"disable_embed" yaml:"disable_embed,omitempty" json:"disable_embed,omitempty" doc:"Disable sending notification as Discord embedded message"`
}

type Gotify struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Gotify server URL" default:""`
	Token       string `koanf:"token" yaml:"token,omitempty" json:"token,omitempty" doc:"Gotify app token" default:""`
	Insecure    bool   `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
	Priority    int    `koanf:"priority" yaml:"priority,omitempty" json:"priority,omitempty" minimum:"0" maximum:"10" doc:"Gotify message priority" default:"0"`
}

type Matrix struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Matrix Homeserver address" default:""`
	Username    string `koanf:"username" yaml:"username,omitempty" json:"username,omitempty" doc:"Matrix username" default:""`
	Password    string `koanf:"password" yaml:"password,omitempty" json:"password,omitempty" doc:"Matrix password" default:""`
	RoomID      string `koanf:"roomid" yaml:"roomid,omitempty" json:"roomid,omitempty" doc:"Room ID to send notifications to" default:""`
	Insecure    bool   `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
}

type Mattermost struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Webhook     string              `koanf:"webhook" yaml:"webhook,omitempty" json:"webhook,omitempty" doc:"Mattermost webhook URL" default:""`
	Channel     string              `koanf:"channel" yaml:"channel,omitempty" json:"channel,omitempty" doc:"Mattermost channel" default:""`
	Priority    string              `koanf:"priority" yaml:"priority,omitempty" json:"priority,omitempty" enum:"standard,important,urgent" doc:"Mattermost message priority" default:"standard"`
	Username    string              `koanf:"username" yaml:"username,omitempty" json:"username,omitempty" doc:"Override Mattermost username for messages"`
	Insecure    bool                `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
	Headers     []map[string]string `koanf:"headers" yaml:"headers,omitempty" json:"headers,omitempty" doc:"HTTP headers to include with Mattermost notifications"`
}

type Ntfy struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string              `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Ntfy Server address" default:""`
	Topic       string              `koanf:"topic" yaml:"topic,omitempty" json:"topic,omitempty" doc:"Ntfy topic" default:""`
	Insecure    bool                `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
	Headers     []map[string]string `koanf:"headers" yaml:"headers,omitempty" json:"headers,omitempty" doc:"HTTP headers to include with Ntfy notifications"`
}

type Pushover struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Token       string `koanf:"token" yaml:"token,omitempty" json:"token,omitempty" doc:"Pushover app token" default:""`
	Userkey     string `koanf:"userkey" yaml:"userkey,omitempty" json:"userkey,omitempty" doc:"Pushover user key" default:""`
	Devices     string `koanf:"devices" yaml:"devices,omitempty" json:"devices,omitempty" doc:"Pushover devices to target for notification" default:""`
	Sound       string `koanf:"sound" yaml:"sound,omitempty" json:"sound,omitempty" doc:"Pushover notification sound" default:"pushover"`
	Priority    int    `koanf:"priority" yaml:"priority,omitempty" json:"priority,omitempty" minimum:"-2" maximum:"2" doc:"Pushover message priority" default:"0"`
	Retry       int    `koanf:"retry" yaml:"retry,omitempty" json:"retry,omitempty" doc:"Retry interval for emergency notifications (Priority 2)" default:"0"`
	Expire      int    `koanf:"expire" yaml:"expire,omitempty" json:"expire,omitempty" doc:"Expiration timer for emergency notifications (Priority 2)" default:"0"`
	TTL         int    `koanf:"ttl" yaml:"ttl,omitempty" json:"ttl,omitempty" doc:"Time to Live for notification messages" default:"0"`
}

type Signal struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string   `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Signal REST API server hostname or IP address" default:""`
	Account     string   `koanf:"account" yaml:"account,omitempty" json:"account,omitempty" doc:"Number of account used to send messages" default:""`
	Recipients  []string `koanf:"recipients" yaml:"recipients,omitempty" json:"recipients,omitempty" doc:"List of recipients to receive messages"`
	Insecure    bool     `koanf:"ignoressl" yaml:"ignoressl,omitempty" enum:"true,false" json:"ignoressl,omitempty" default:"false"`
}

type SMTP struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"SMTP server hostname or IP address" default:""`
	Port        int    `koanf:"port" yaml:"port,omitempty" json:"port,omitempty" minimum:"1" maximum:"65535" doc:"SMTP server port" default:"25"`
	TLS         bool   `koanf:"tls" yaml:"tls,omitempty" json:"tls,omitempty" enum:"true,false" doc:"Enable/Disable TLS connection" default:"false"`
	AuthType    string `koanf:"authtype" yaml:"authtype,omitempty" json:"authtype,omitempty" enum:"plain,plain-noenc,login,login-noenc,noauth,cram-md5,xoauth2,scram-sha-1,scram-sha-1-plus,scram-sha-256,scram-sha-256-plus,autodiscover" doc:"SMTP Authentication type" default:"plain"`
	User        string `koanf:"user" yaml:"user,omitempty" json:"user,omitempty" doc:"SMTP user for authentication" default:""`
	Password    string `koanf:"password" yaml:"password,omitempty" json:"password,omitempty" doc:"SMTP password for authentication" default:""`
	From        string `koanf:"from" yaml:"from,omitempty" json:"from,omitempty" format:"email" doc:"SMTP sender" default:""`
	Recipient   string `koanf:"recipient" yaml:"recipient,omitempty" json:"recipient,omitempty" format:"email" doc:"SMTP recipient" default:""`
	Thread      string `koanf:"thread" yaml:"thread" json:"thread" enum:"day,camera" doc:"Specify method of email threading" default:"day"`
	Insecure    bool   `koanf:"ignoressl" yaml:"ignoressl,omitempty" enum:"true,false" json:"ignoressl,omitempty" default:"false"`
	MsgUUID     bool   `koanf:"msguuid" yaml:"msguuid,omitempty" json:"msguuid,omitempty" enum:"true,false" doc:"Generate message-id from uuid" default:"false"`
}

type Telegram struct {
	AlertCommon              `koanf:",squash" yaml:",inline"`
	ChatID                   int64  `koanf:"chatid" yaml:"chatid,omitempty" json:"chatid,omitempty" doc:"Telegram chat ID" default:"0"`
	MessageThreadID          int    `koanf:"message_thread_id" yaml:"message_thread_id,omitempty" json:"message_thread_id,omitempty" doc:"Send message to thread by ID" default:"0"`
	Token                    string `koanf:"token" yaml:"token,omitempty" json:"token,omitempty" doc:"Telegram bot token" default:""`
	SendClip                 bool   `koanf:"send_clip" yaml:"send_clip,omitempty" json:"send_clip,omitempty" doc:"Send event video clip instead of snapshot image" default:"false"`
	SendClipAfterSnapshot    bool   `koanf:"send_clip_after_snapshot" yaml:"send_clip_after_snapshot,omitempty" json:"send_clip_after_snapshot,omitempty" doc:"Send snapshot first, then replace it with the event video clip once available (within clip_max_size)" default:"false"`
	ClipMaxSize              int    `koanf:"clip_max_size" yaml:"clip_max_size,omitempty" json:"clip_max_size,omitempty" doc:"Maximum clip size (MB) to upload when replacing the snapshot. Telegram's cloud Bot API caps uploads at 50 MB; a self-hosted local Bot API server allows up to 2000 MB" default:"50"`
	ClipTranscode            bool   `koanf:"clip_transcode" yaml:"clip_transcode,omitempty" json:"clip_transcode,omitempty" doc:"Re-encode the clip with ffmpeg to a smaller H.264/AAC video before uploading it (requires ffmpeg)" default:"false"`
	ClipTranscodeHeight      int    `koanf:"clip_transcode_height" yaml:"clip_transcode_height,omitempty" json:"clip_transcode_height,omitempty" doc:"Maximum height (pixels) of the transcoded clip. Smaller clips are not upscaled" default:"720"`
	ClipTranscodeBitrate     int    `koanf:"clip_transcode_bitrate" yaml:"clip_transcode_bitrate,omitempty" json:"clip_transcode_bitrate,omitempty" doc:"Maximum video bitrate (kbit/s) of the transcoded clip" default:"1000"`
	ClipTranscodeTimeout     int    `koanf:"clip_transcode_timeout" yaml:"clip_transcode_timeout,omitempty" json:"clip_transcode_timeout,omitempty" doc:"Seconds to wait for ffmpeg before giving up on the clip" default:"120"`
	ClipTranscodeVAAPIDevice string `koanf:"clip_transcode_vaapi_device" yaml:"clip_transcode_vaapi_device,omitempty" json:"clip_transcode_vaapi_device,omitempty" doc:"VAAPI render device (e.g. /dev/dri/renderD128) to transcode on the GPU, falling back to software if it fails" default:""`
}

type Webhook struct {
	AlertCommon `koanf:",squash" yaml:",inline"`
	Server      string              `koanf:"server" yaml:"server,omitempty" json:"server,omitempty" doc:"Webhook address" default:""`
	Insecure    bool                `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"insecure,omitempty" doc:"Ignore TLS/SSL errors" default:"false"`
	Method      string              `koanf:"method" yaml:"method,omitempty" json:"method,omitempty" enum:"GET,POST" doc:"HTTP method for webhook notifications" default:"POST"`
	Params      []map[string]string `fix:"params" json:"params,omitempty" yaml:"params,omitempty" doc:"URL parameters for webhook notifications"`
	Headers     []map[string]string `koanf:"headers" yaml:"headers,omitempty" json:"headers,omitempty" doc:"HTTP headers for webhook notifications"`
	Template    interface{}         `koanf:"template" yaml:"template,omitempty" json:"template,omitempty" doc:"Custom message template"`
}

// webhookYAML is the YAML shape of a webhook profile. yaml.v3 panics on the
// Webhook struct itself because the embedded AlertCommon.Template and the
// outer Template field both map to the "template" key. This mirrors the JSON
// behavior, where the outer field shadows the embedded one: "template" holds
// the custom JSON body, and a template name on a webhook is meaningless (the
// webhook sender never renders a named template).
type webhookYAML struct {
	Enabled  bool                `yaml:"enabled,omitempty"`
	Title    string              `yaml:"title,omitempty"`
	Filters  AlertFilter         `yaml:"filters,omitempty"`
	Server   string              `yaml:"server,omitempty"`
	Insecure bool                `yaml:"ignoressl,omitempty"`
	Method   string              `yaml:"method,omitempty"`
	Params   []map[string]string `yaml:"params,omitempty"`
	Headers  []map[string]string `yaml:"headers,omitempty"`
	Template interface{}         `yaml:"template,omitempty"`
}

// MarshalYAML renders a webhook profile through webhookYAML.
func (w Webhook) MarshalYAML() (interface{}, error) {
	return webhookYAML{
		Enabled:  w.Enabled,
		Title:    w.Title,
		Filters:  w.Filters,
		Server:   w.Server,
		Insecure: w.Insecure,
		Method:   w.Method,
		Params:   w.Params,
		Headers:  w.Headers,
		Template: w.Template,
	}, nil
}

// UnmarshalYAML restores a webhook profile from webhookYAML.
func (w *Webhook) UnmarshalYAML(value *yaml.Node) error {
	var aux webhookYAML
	if err := value.Decode(&aux); err != nil {
		return err
	}
	w.Enabled = aux.Enabled
	w.Title = aux.Title
	w.Filters = aux.Filters
	w.Server = aux.Server
	w.Insecure = aux.Insecure
	w.Method = aux.Method
	w.Params = aux.Params
	w.Headers = aux.Headers
	w.Template = aux.Template
	return nil
}

type Monitor struct {
	Enabled  bool   `koanf:"enabled" yaml:"enabled" json:"enabled" enum:"true,false" doc:"Enable monitoring via external uptime application" default:"false"`
	URL      string `koanf:"url" yaml:"url,omitempty" json:"url,omitempty" doc:"Address of monitoring server" default:""`
	Interval int    `koanf:"interval" yaml:"interval,omitempty" json:"interval,omitempty" minimum:"1" maximum:"10000000" doc:"Interval between check-in messages" default:"60"`
	Insecure bool   `koanf:"ignoressl" yaml:"ignoressl,omitempty" json:"ignoressl,omitempty" enum:"true,false" doc:"Ignore TLS/SSL errors" default:"false"`
}
