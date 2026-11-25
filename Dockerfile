FROM golang:1.25-alpine AS build

WORKDIR /app

COPY go.mod /app/
COPY go.sum /app/

# gcc & musl-dev required for matrix support
RUN apk --no-cache add tzdata ca-certificates gcc musl-dev
RUN go mod download

COPY . /app/

# Tag goolm & ldflags required for matrix support
RUN go build -tags "goolm" -a -ldflags '-linkmode external -extldflags "-static"' -o /frigate-notify-plus .

FROM alpine:3.24

WORKDIR /app

# ffmpeg transcodes Telegram clips (clip_transcode). The Intel VA driver backs
# clip_transcode_vaapi_device & is only packaged for x86_64
RUN apk --no-cache add ffmpeg tzdata ca-certificates \
    && if [ "$(apk --print-arch)" = "x86_64" ]; then apk --no-cache add intel-media-driver; fi

COPY --from=build /frigate-notify-plus /app/frigate-notify-plus
# Factory templates for the legacy layout (no data directory): the binary
# also embeds them, so this copy only matters when FN_DATA_DIR is empty and
# the app reads the "templates" directory next to the working directory.
COPY /templates /app/templates

# Persistent data directory: config.yml, logs/, mute.json and templates/
# live here.
# Mount a host directory on /data so config and logs survive container
# recreation; deleting the container does not delete the data.
ENV FN_DATA_DIR=/data
RUN mkdir -p /data
VOLUME /data

EXPOSE 8000

ENTRYPOINT [ "/app/frigate-notify-plus" ]
