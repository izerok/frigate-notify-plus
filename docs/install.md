# Install / Setup

Configuration can be provided via config file or environment variables, check [here](config/file.md) for details. A configuration file template can be found [here](config/sample.md)

Most configuration can also be done through the built-in Web UI console — see [First run](#first-run) below.

## Docker (Recommended)

The image is published on the GitHub Container Registry (GHCR). Pull it and run it with the host network so it can reach Frigate and MQTT on the LAN:

```bash
docker pull ghcr.io/izerok/frigate-notify-plus:v0.1.0
docker run -d --name frigate-notify-plus --network host \
  -v /srv/frigate-notify-plus:/data \
  -e TZ=Asia/Shanghai \
  ghcr.io/izerok/frigate-notify-plus:v0.1.0
```

Mount a host directory on `/data` (replace `/srv/frigate-notify-plus` with your path). The data directory holds `config.yml`, `logs/`, and `mute.json`, so the config and logs survive container recreation — deleting the container does not delete the data. No `config.yml` file mount is needed.

Or with compose:

```yaml
services:
  frigate-notify-plus:
    image: ghcr.io/izerok/frigate-notify-plus:v0.1.0
    container_name: frigate-notify-plus
    network_mode: host
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - /srv/frigate-notify-plus:/data
    restart: unless-stopped
```

### Tarball install (offline)

Each release also ships the Docker image as a `.tar.gz` file, published on the [Releases](https://github.com/izerok/frigate-notify-plus/releases) page alongside `sha256sums.txt`, for hosts without registry access. Download the tar for the version you want, verify its checksum, then load and run it:

```bash
curl -LO https://github.com/izerok/frigate-notify-plus/releases/download/<tag>/<tar.gz>
sha256sum -c sha256sums.txt
docker load -i <tar.gz>
```

The image inside the tar is tagged `frigate-notify-plus:webui-<short-sha>` (for example `frigate-notify-plus:webui-52e7e9a`). Run it the same way as above, using that local image tag.

The data directory can be changed with the `FN_DATA_DIR` environment variable (or the `-d` flag), and the config file location can still be overridden with the `FN_CONFIGFILE` environment variable (or the `-c` flag) — for example, to keep using a single mounted config file:

```yaml
services:
  frigate-notify-plus:
    image: ghcr.io/izerok/frigate-notify-plus:v0.1.0
    network_mode: host
    environment:
      FN_CONFIGFILE: /app/config/config.yml
      TZ: Asia/Shanghai
    volumes:
      - /path/to/config_directory:/app/config
    restart: unless-stopped
```

!!!note
    Sensitive information (like passwords) can be configured via environment variables or docker secrets.

    Environment variables are listed in the [config file reference](config/file.md).

    For docker secrets, configure your secret - then point the appropriate environment variable to your secret file. For example, if you had a secret named `mqtt_password` and wanted to provide that to frigate-notify-plus:

    ```yaml
    services:
      frigate-notify-plus:
        image: ghcr.io/izerok/frigate-notify-plus:v0.1.0
        environment:
          FN_FRIGATE__MQTT__PASSWORD: /run/secrets/mqtt_password
    ```

## First run

The Web UI console runs on port `8000` and is enabled by default. Open `http://<host>:8000` in a browser on your LAN.

If no config exists in the data directory yet, the app starts in setup mode and the wizard walks you through:

1. Frigate server address (it checks the address answers `/api/version`)
2. Event source — MQTT or the Frigate Web API
3. Your first notification channel (for example Telegram)
4. A test notification

The console has no authentication and is intended for LAN use only — do not expose port 8000 publicly.

Scheduled GenAI summaries run on the container's local time. Set the container timezone to match your Frigate host, for example `-e TZ=Asia/Shanghai`.

## Local

No standalone binaries are published for this fork. To run without Docker, build from source (Go 1.25+):

```bash
go build -tags goolm -o frigate-notify-plus .
./frigate-notify-plus
```

By default, the app will look for a config file in the same directory named `config.yml`. With a data directory set (`-d <dir>` or `FN_DATA_DIR`), the config file is `<dir>/config.yml` and logs go to `<dir>/logs/frigate-notify.log` (rotated at 10 MB, 5 backups kept).

You can manually specify a config file name or location with the `-c` flag:

```bash
./frigate-notify-plus -c <new-config-file-location>
```

Or use a data directory:

```bash
./frigate-notify-plus -d /srv/frigate-notify-plus
```
