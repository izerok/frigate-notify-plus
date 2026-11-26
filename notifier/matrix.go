package notifier

import (
	"context"
	"io"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	evt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
	"github.com/izerok/frigate-notify-plus/util/proxyx"
)

// SendMatrix pushes alert message to Matrix chat via webhook
func SendMatrix(event models.Event, snapshot io.Reader, provider notifMeta) {
	profile := config.ConfigData.Alerts.Matrix[provider.index]
	status := &config.Internal.Status.Notifications.Matrix[provider.index]

	var err error
	var message string
	// Build notification
	if profile.Template != "" {
		message = renderMessage(profile.Template, event, "message", "Matrix")
	} else {
		message = renderMessage("html", event, "message", "Matrix")
	}

	// New matrix client
	m, err := mautrix.NewClient(profile.Server, "", "")
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Matrix").
			Err(err).
			Int("provider_id", provider.index).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}

	// Route the client's requests through the profile's proxy (if any)
	// and apply the ignore-SSL flag. ExternalClient covers media fetched
	// from outside the homeserver, so it gets the same client. When
	// neither applies, leave mautrix's own client alone: it uses a 180 s
	// timeout, and replacing it with a short one would break slow
	// homeservers and media transfers.
	if proxyx.Configured(profile.Proxy) || profile.Insecure {
		proxyClient := proxyx.Client(profile.Proxy, 180*time.Second, profile.Insecure)
		m.Client = proxyClient
		m.ExternalClient = proxyClient
	}

	// Handle login
	m.StateStore = mautrix.NewMemoryStateStore()
	ch, err := cryptohelper.NewCryptoHelper(m, []byte("asdf"), "./matrix.db")
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Matrix").
			Err(err).
			Int("provider_id", provider.index).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}
	ch.LoginAs = &mautrix.ReqLogin{
		Type:       mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: profile.Username},
		Password:   profile.Password,
	}
	ch.Init(context.Background())
	m.Crypto = ch

	// Join room if needed
	_, err = m.JoinRoomByID(context.Background(), id.RoomID(profile.RoomID))
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Matrix").
			Int("provider_id", provider.index).
			Err(err).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}

	// Send snapshot image if available
	if event.HasSnapshot {
		var media *mautrix.RespMediaUpload
		img, _ := io.ReadAll(snapshot)
		media, _ = m.UploadMedia(context.Background(), mautrix.ReqUploadMedia{ContentBytes: img, ContentType: "image/jpeg", FileName: "snapshot.jpg"})
		_, err = m.SendMessageEvent(context.Background(), id.RoomID(profile.RoomID), evt.EventMessage, &evt.MessageEventContent{
			MsgType: evt.MsgImage,
			Body:    "snapshot.jpg",
			URL:     media.ContentURI.CUString(),
			Info:    &evt.FileInfo{MimeType: "image/jpeg"},
		})
		if err != nil {
			log.Warn().
				Str("event_id", event.ID).
				Str("provider", "Matrix").
				Int("provider_id", provider.index).
				Err(err).
				Msg("Unable to send alert")
			status.NotifFailure(err.Error())
			return
		}
	}

	// Send event details
	_, err = m.SendMessageEvent(context.Background(), id.RoomID(profile.RoomID), evt.EventMessage, &evt.MessageEventContent{
		MsgType:       evt.MsgText,
		Format:        "org.matrix.custom.html",
		FormattedBody: message,
	})
	if err != nil {
		log.Warn().
			Str("event_id", event.ID).
			Str("provider", "Matrix").
			Int("provider_id", provider.index).
			Err(err).
			Msg("Unable to send alert")
		status.NotifFailure(err.Error())
		return
	}

	log.Info().
		Str("event_id", event.ID).
		Str("provider", "Matrix").
		Int("provider_id", provider.index).
		Msg("Alert sent")
	status.NotifSuccess()
}
