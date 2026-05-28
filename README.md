# tape

`tape` is a small Gluetun/qBittorrent port watcher. It keeps qBittorrent's listen port aligned with Gluetun's active forwarded VPN port.

The app is the sole qBittorrent sync owner. Gluetun still owns the VPN tunnel, firewall, and forwarded-port acquisition; this app only observes Gluetun's control API, updates qBittorrent when needed, and asks Gluetun to reacquire a port after repeated missing-port reads.

## Safety Model

This sidecar assumes:

- Gluetun's kill switch is enabled and remains the traffic safety boundary.
- qBittorrent shares Gluetun's network namespace, for example with `network_mode: service:gluetun`.
- qBittorrent has no local proxy or alternate egress path that bypasses Gluetun.

Before making this app the solo sync owner, remove any Gluetun `VPN_PORT_FORWARDING_UP_COMMAND` and `VPN_PORT_FORWARDING_DOWN_COMMAND` settings. Those hooks can race this app and make qBittorrent port state ambiguous.

The app never sets qBittorrent's `listen_port` to `0`.

## Configuration

Every setting can be provided by environment variable or flag. Flags override environment variables.

| Environment variable | Flag | Default | Description |
| --- | --- | --- | --- |
| `GLUETUN_URL` | `--gluetun-url` | none | Required Gluetun control server URL. |
| `QBIT_URL` | `--qbit-url` | none | Required qBittorrent Web UI URL. |
| `GLUETUN_API_KEY_FILE` | `--gluetun-api-key-file` | none | Required file containing the Gluetun control API key or Gluetun auth TOML with `apikey = "..."`. |
| `PORTWATCH_INTERVAL` | `--interval` | `1m` | Poll interval for Gluetun forwarded-port checks. |
| `PORTWATCH_FAILURES` | `--failures` | `5` | Consecutive missing-port reads before requesting VPN reacquisition. |
| `PORTWATCH_COOLDOWN` | `--cooldown` | `3m` | Cooldown after a reacquisition attempt before another attempt is allowed. |
| `PORTWATCH_HTTP_TIMEOUT` | `--http-timeout` | `10s` | Timeout for Gluetun and qBittorrent HTTP calls. |
| `PORTWATCH_QBIT_AUDIT_INTERVAL` | `--qbit-audit-interval` | `30m` | Periodic qBittorrent listen-port audit interval. |
| `QBIT_INTERFACE` | `--qbit-interface` | `tun0` | qBittorrent network interface value to set with the listen port. |
| none | `--once` | `false` | Run one poll/sync cycle and exit. |
| none | `--dry-run` | `false` | Log intended actions without changing qBittorrent or Gluetun state. |

Durations use Go duration syntax, for example `30s`, `1m`, or `2h45m`.

## qBittorrent Cache Behavior

On startup, the app reads qBittorrent's current listen port once and caches it. During normal operation it polls only Gluetun, which keeps the steady-state loop small and avoids repeatedly reading qBittorrent when Gluetun's forwarded port is unchanged.

The app periodically audits qBittorrent according to `PORTWATCH_QBIT_AUDIT_INTERVAL` / `--qbit-audit-interval`. It also revalidates qBittorrent after uncertainty, such as a failed qBittorrent update or a Gluetun VPN reacquisition. If Gluetun reports no valid forwarded port, the app records that state and does not write `0` to qBittorrent.

## Local Commands

```sh
go test ./...
go run ./cmd/gluetun-portwatch --once --dry-run
docker build --platform linux/amd64 -t gluetun-portwatch:local .
```

The `go run` example still needs the required configuration through environment variables or matching flags.

## Container Image

Build locally:

```sh
docker build --platform linux/amd64 -t gluetun-portwatch:local .
```

The GitHub Actions workflow publishes images to:

```text
ghcr.io/kedar42/tape
```

on pushes to `main` and `v*` tags after tests pass. Pull requests run tests but do not push images.

## Compose Example

This example runs the sidecar on a Docker network that can reach both Gluetun and qBittorrent, mounts Gluetun's auth TOML read-only, and does not mount the Docker socket.

```yaml
services:
  tape:
    image: ghcr.io/kedar42/tape:latest
    container_name: tape
    restart: unless-stopped
    networks:
      - app_net
    environment:
      GLUETUN_URL: http://gluetun:8000
      QBIT_URL: http://gluetun:8080
      GLUETUN_API_KEY_FILE: /run/secrets/gluetun-auth.toml
    volumes:
      - ./config/gluetun/auth.toml:/run/secrets/gluetun-auth.toml:ro

networks:
  app_net:
    external: true
```

Make sure qBittorrent is still routed through Gluetun's network namespace and not through this sidecar. This app needs HTTP reachability to Gluetun and qBittorrent only; it does not need the Docker socket.
