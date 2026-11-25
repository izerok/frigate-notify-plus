package apiv1

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

var V1_PREFIX string

func Registerv1Routes(api huma.API) {
	V1_PREFIX = "/api/v1"

	// GET /readyz
	huma.Register(api, huma.Operation{
		OperationID: "get-readyz",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/readyz",
		Hidden:      true,
		Summary:     V1_PREFIX + "/readyz",
		Description: "Retrieve Frigate-Notify-Plus ready state",
		Tags:        []string{"Status"},
	}, GetReadyz)

	// GET /healthz
	huma.Register(api, huma.Operation{
		OperationID: "get-healthz",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/healthz",
		Hidden:      true,
		Summary:     V1_PREFIX + "/healthz",
		Description: "Retrieve Frigate-Notify-Plus liveness state",
		Tags:        []string{"Status"},
	}, GetHealthz)

	// GET /version
	huma.Register(api, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/version",
		Summary:     V1_PREFIX + "/version",
		Description: "Retrieve Frigate-Notify-Plus application version",
		Tags:        []string{"Status"},
	}, GetVersion)

	// GET /status
	huma.Register(api, huma.Operation{
		OperationID: "get-status",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/status",
		Summary:     V1_PREFIX + "/status",
		Description: "Retrieve detailed health and status of Frigate-Notify-Plus",
		Tags:        []string{"Status"},
	}, GetStatus)

	// GET /config
	huma.Register(api, huma.Operation{
		OperationID: "get-config",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/config",
		Summary:     V1_PREFIX + "/config",
		Description: "Retrieve current running configuration",
		Tags:        []string{"Config"},
	}, GetConfig)

	// PUT /config
	huma.Register(api, huma.Operation{
		OperationID:   "put-config",
		Method:        http.MethodPut,
		Path:          V1_PREFIX + "/config",
		Summary:       V1_PREFIX + "/config",
		Description:   "Set current running configuration",
		Tags:          []string{"Config"},
		DefaultStatus: http.StatusAccepted,
	}, PutConfig)

	// POST /reload
	huma.Register(api, huma.Operation{
		OperationID:   "post-reload",
		Method:        http.MethodPost,
		Path:          V1_PREFIX + "/reload",
		Summary:       V1_PREFIX + "/reload",
		Description:   "Reload config from file & restart app",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusAccepted,
	}, PostReload)

	// GET /notif_state
	huma.Register(api, huma.Operation{
		OperationID: "get-notif-state",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/notif_state",
		Summary:     V1_PREFIX + "/notif_state",
		Description: "Retrieve current state of Frigate-Notify-Plus notifications",
		Tags:        []string{"Control"},
	}, GetNotifState)

	// POST /notif_state
	huma.Register(api, huma.Operation{
		OperationID:   "post-notif_state",
		Method:        http.MethodPost,
		Path:          V1_PREFIX + "/notif_state",
		Summary:       V1_PREFIX + "/notif_state",
		Description:   "Set state of Frigate-Notify-Plus alerting",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusAccepted,
	}, PostNotifState)

	// POST /notiftest
	huma.Register(api, huma.Operation{
		OperationID:   "post-notiftest",
		Method:        http.MethodPost,
		Path:          V1_PREFIX + "/notif_test",
		Summary:       V1_PREFIX + "/notif_test",
		Description:   "Send test notification via configured providers",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusAccepted,
	}, PostNotifTest)

	// POST /genai_summarize/run
	huma.Register(api, huma.Operation{
		OperationID:   "post-genai_summarize-run",
		Method:        http.MethodPost,
		Path:          V1_PREFIX + "/genai_summarize/run",
		Summary:       V1_PREFIX + "/genai_summarize/run",
		Description:   "Run one scheduled GenAI summary immediately (by index in alerts.general.genai.schedules)",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusOK,
	}, RunGenAISchedule)

	// GET /history
	huma.Register(api, huma.Operation{
		OperationID: "get-history",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/history",
		Summary:     V1_PREFIX + "/history",
		Description: "Retrieve recent dispatched notifications (in-memory, newest first)",
		Tags:        []string{"Status"},
	}, GetHistory)

	// GET /templates
	huma.Register(api, huma.Operation{
		OperationID: "get-templates",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/templates",
		Summary:     V1_PREFIX + "/templates",
		Description: "Retrieve current message templates",
		Tags:        []string{"Config"},
	}, GetTemplates)

	// PUT /templates
	huma.Register(api, huma.Operation{
		OperationID:   "put-templates",
		Method:        http.MethodPut,
		Path:          V1_PREFIX + "/templates",
		Summary:       V1_PREFIX + "/templates",
		Description:   "Replace message templates (validated, then written to disk)",
		Tags:          []string{"Config"},
		DefaultStatus: http.StatusAccepted,
	}, PutTemplates)

	// GET /templates/defaults
	huma.Register(api, huma.Operation{
		OperationID: "get-templates-defaults",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/templates/defaults",
		Summary:     V1_PREFIX + "/templates/defaults",
		Description: "Retrieve factory (embedded) message templates, ignoring disk edits",
		Tags:        []string{"Config"},
	}, GetTemplateDefaults)

	// POST /templates/render
	huma.Register(api, huma.Operation{
		OperationID: "post-templates-render",
		Method:      http.MethodPost,
		Path:        V1_PREFIX + "/templates/render",
		Summary:     V1_PREFIX + "/templates/render",
		Description: "Render a message template against the sample event",
		Tags:        []string{"Config"},
	}, PostRenderTemplate)

	// GET /mute
	huma.Register(api, huma.Operation{
		OperationID: "get-mute",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/mute",
		Summary:     V1_PREFIX + "/mute",
		Description: "List currently muted cameras",
		Tags:        []string{"Control"},
	}, GetMute)

	// POST /mute
	huma.Register(api, huma.Operation{
		OperationID:   "post-mute",
		Method:        http.MethodPost,
		Path:          V1_PREFIX + "/mute",
		Summary:       V1_PREFIX + "/mute",
		Description:   "Mute a camera for a number of minutes",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusAccepted,
	}, PostMute)

	// DELETE /mute
	huma.Register(api, huma.Operation{
		OperationID:   "delete-mute",
		Method:        http.MethodDelete,
		Path:          V1_PREFIX + "/mute",
		Summary:       V1_PREFIX + "/mute",
		Description:   "Remove a camera's mute",
		Tags:          []string{"Control"},
		DefaultStatus: http.StatusAccepted,
	}, DeleteMute)

	// POST /frigate_check
	huma.Register(api, huma.Operation{
		OperationID: "post-frigate-check",
		Method:      http.MethodPost,
		Path:        V1_PREFIX + "/frigate_check",
		Summary:     V1_PREFIX + "/frigate_check",
		Description: "Probe a Frigate server's version endpoint",
		Tags:        []string{"Control"},
	}, PostFrigateCheck)

	// GET /frigate_meta
	huma.Register(api, huma.Operation{
		OperationID: "get-frigate-meta",
		Method:      http.MethodGet,
		Path:        V1_PREFIX + "/frigate_meta",
		Summary:     V1_PREFIX + "/frigate_meta",
		Description: "Retrieve Frigate cameras, zones, labels, sub-labels, MQTT settings and version",
		Tags:        []string{"Config"},
	}, GetFrigateMeta)
}
