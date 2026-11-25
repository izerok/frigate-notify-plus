package apiv1

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/izerok/frigate-notify-plus/notifier"
	"github.com/rs/zerolog/log"
)

type MuteOutput struct {
	Body struct {
		Mutes []notifier.MuteEntry `json:"mutes"`
	}
}

type PostMuteInput struct {
	Body struct {
		Camera  string `json:"camera"`
		Minutes int    `json:"minutes"`
	}
}

type PostMuteOutput struct {
	Body struct {
		Status string    `json:"status"`
		Camera string    `json:"camera"`
		Until  time.Time `json:"until"`
	}
}

// DeleteMuteInput takes the camera as a query string because huma rejects
// pointer query parameters.
type DeleteMuteInput struct {
	Camera string `query:"camera" doc:"Camera to unmute."`
}

type DeleteMuteOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// GetMute lists the currently muted cameras, newest mute first by camera
// name. The list is always an array, never null.
func GetMute(ctx context.Context, input *struct{}) (*MuteOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/mute").
		Str("method", "GET").
		Msg("Received API request")

	resp := &MuteOutput{}
	resp.Body.Mutes = notifier.ListMutes()
	if resp.Body.Mutes == nil {
		resp.Body.Mutes = []notifier.MuteEntry{}
	}
	return resp, nil
}

// PostMute mutes a camera for the given number of minutes (1..4320).
func PostMute(ctx context.Context, input *PostMuteInput) (*PostMuteOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/mute").
		Str("method", "POST").
		Msg("Received API request")

	if input.Body.Camera == "" {
		return nil, huma.Error422UnprocessableEntity("camera must not be empty")
	}
	until, err := notifier.MuteCamera(input.Body.Camera, input.Body.Minutes)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &PostMuteOutput{}
	resp.Body.Status = "ok"
	resp.Body.Camera = input.Body.Camera
	resp.Body.Until = until
	return resp, nil
}

// DeleteMute removes a camera's mute. Unmuting a camera that is not muted
// is still ok.
func DeleteMute(ctx context.Context, input *DeleteMuteInput) (*DeleteMuteOutput, error) {
	log.Trace().
		Str("uri", V1_PREFIX+"/mute").
		Str("method", "DELETE").
		Msg("Received API request")

	if input.Camera == "" {
		return nil, huma.Error422UnprocessableEntity("camera query parameter is required")
	}
	notifier.UnmuteCamera(input.Camera)

	resp := &DeleteMuteOutput{}
	resp.Body.Status = "ok"
	return resp, nil
}
