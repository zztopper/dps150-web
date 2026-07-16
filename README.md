# dps150-web

Web-based control panel for the **FNIRSI DPS-150** programmable DC power supply
(0–30 V / 0–5 A / 150 W, USB-CDC serial). Drive the supply from your desktop or
your phone, watch live telemetry and a month of history, get Telegram alerts,
and script it over a token-authenticated REST API.

![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)
![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)
![React 19](https://img.shields.io/badge/react-19-61DAFB.svg)

📖 **[Русская версия README →](README.ru.md)**

The device connects directly over a serial port, over a serial-over-TCP bridge
(ser2net), or to a built-in emulator — so you can run and develop the whole
stack with no hardware attached.

## Features

- **Full supply control** — voltage/current setpoints, output on/off (with
  confirmation), protection limits (OVP / OCP / OPP / OTP / LVP) and hardware
  presets M1–M6.
- **Named profiles** — store V + I + protection sets in the database and assign
  them to the device's M1–M6 memory cells. Applying a profile never turns the
  output on by itself.
- **Live charts + history** — 2 Hz telemetry (V/I/P, input voltage, temperature,
  CC/CV, protection state) rendered with uPlot, plus one month of retained
  history with minute-level aggregation and zoom/pan.
- **Event log** — protection trips, output switching, connect/disconnect, all
  timestamped.
- **Telegram notifications** — protection trips, device link loss/recovery and
  auto-stop events; notification types are configured in the UI.
- **Energy metering** — Ah / Wh counters straight from the device.
- **Auto-stop rules** — stop the output on a condition (current below a
  threshold for N seconds, after a charge/energy total, or after a time limit),
  backed by the hardware protections.
- **Programmable sequences** — build test programs from a tree of steps:
  *set-and-hold* (drive V/I until a condition holds), *ramp* (sweep V or I
  over time) and nested *loop* blocks. Run them for battery charge/discharge
  cycling, burn-in or characterisation; starting a run energises the output
  and every exit path (finish, stop, protection trip, restart) switches it off
  again, with live step-by-step progress and a 409 guard that blocks manual
  control while a program owns the device.
- **CSV export** — download the current history or event view as CSV.
- **API tokens** — Bearer-token access for scripts (scoped `read` / `control`),
  see the auth model below.
- **Mobile-friendly + dark theme** — a large-digit bench view for the phone and
  the full desktop UI, in light or dark mode.
- **i18n** — Russian and English.
- **Home Assistant + metrics** — MQTT Discovery publishes the supply as HA
  sensors (and, opt-in, a controllable output and setpoints); Prometheus
  telemetry gauges feed a ready-made Grafana dashboard.

## Screenshots

Live control panel, driven by the built-in device emulator (light theme; a dark
theme ships too).

### Dashboard — live readings, setpoints, protections, quick profiles and metering

![Dashboard](docs/screenshots/dashboard.png)

### History — a month of telemetry with uPlot charts, range presets and CSV export

![History](docs/screenshots/history.png)

### Sequences — programmable test programs (set-and-hold / ramp / nested loops)

![Sequences](docs/screenshots/sequences.png)

The step-tree editor — a CC-CV charge, a discharge, then a 3× ramp/hold loop:

![Sequence editor](docs/screenshots/sequences-editor.png)

### Automation — auto-stop rules over the telemetry stream

![Automation](docs/screenshots/automation.png)

<details>
<summary>More screens — Profiles, Events, Settings</summary>

**Profiles** — named V/I + protection presets, apply to the device or a hardware M1–M6 slot

![Profiles](docs/screenshots/profiles.png)

**Events** — the device journal (protection trips, output changes, connects, auto-stops)

![Events](docs/screenshots/events.png)

**Settings** — Telegram notification preferences and API tokens for scripted access

![Settings](docs/screenshots/settings.png)

</details>

## Quick start

### From source (single binary)

Requirements: **Go 1.25+**, **Node.js 20+**.

```bash
# Build the frontend bundle and the backend binary with the UI embedded.
make build
```

`make build` produces `backend/bin/dps150-server` with the production frontend
bundle embedded via `go:embed`, so the single binary serves both the API and
the web UI on `:8080`.

Run it against the built-in emulator (no hardware required):

```bash
./backend/bin/dps150-server -transport mock://
# open http://localhost:8080
```

Run it against a real DPS-150 attached over USB:

```bash
./backend/bin/dps150-server -transport serial:///dev/ttyUSB0
# macOS example: -transport serial:///dev/tty.usbmodem101
```

Or against a remote serial-over-TCP bridge (see `docs/runbooks/ser2net-pve.md`):

```bash
./backend/bin/dps150-server -transport tcp://192.0.2.10:2150
```

### Standalone container (SQLite, no PostgreSQL)

A single container that serves the UI and the API from one binary, storing
history in SQLite — nothing else to run. Built from
[`deploy/docker/Dockerfile.standalone`](deploy/docker/Dockerfile.standalone):

```bash
docker build -f deploy/docker/Dockerfile.standalone -t dps150-web .
docker run --rm -p 8080:8080 -v dps150-data:/data dps150-web
# open http://localhost:8080  (emulator by default)
```

The `dps150-data` volume keeps the SQLite database across restarts. Point it at
a real PSU with `--device /dev/ttyUSB0 -e DPS_TRANSPORT=serial:///dev/ttyUSB0`.

### Docker Compose (PostgreSQL, prebuilt images)

Brings up the backend, the nginx-served frontend and PostgreSQL, pulling the
images published to Docker Hub by CI:

```bash
docker compose pull
docker compose up -d
open http://localhost:8081
```

By default the stack runs the emulator (`DPS_TRANSPORT=mock://`). To pass a real
USB device through to the container:

```bash
BACKEND_UPSTREAM=backend-serial:8080 docker compose --profile serial up -d
```

Storage is fail-soft: the backend starts and controls the device even if
PostgreSQL is down; storage-backed features return `503` until it recovers.

## Configuration

The backend is configured via command-line flags or environment variables (a
flag wins over its variable). Notification credentials are environment-only.

| Variable | Flag | Default | Description |
|---|---|---|---|
| `DPS_TRANSPORT` | `-transport` | `mock://` | Device transport: `serial:///dev/ttyUSB0`, `tcp://host:port` or `mock://` |
| `DPS_LISTEN_ADDR` | `-listen` | `:8080` | HTTP listen address |
| `DPS_LOG_LEVEL` | `-log-level` | `info` | `debug`, `info`, `warn` or `error` |
| `DPS_DB_DRIVER` | `-db-driver` | `sqlite` | Storage backend: `sqlite` or `postgres` |
| `DPS_DB_DSN` | `-db-dsn` | `dps150.db` | File path for sqlite, `postgres://user:pass@host:port/db` for postgres |
| `DPS_AUTH_REQUIRED` | `-auth-required` | `false` | Require a Bearer token or an Authelia `Remote-User` header on `/api` |
| `DPS_TELEGRAM_TOKEN` | — | _(empty)_ | Telegram bot token; notifications are disabled when empty |
| `DPS_TELEGRAM_CHAT_ID` | — | _(empty)_ | Telegram chat ID for notifications |
| `DPS_MQTT_BROKER` | — | _(empty)_ | MQTT broker URL (`tcp://host:1883`); the Home Assistant integration is disabled when empty |
| `DPS_MQTT_USERNAME` / `DPS_MQTT_PASSWORD` | — | _(empty)_ | MQTT broker credentials |
| `DPS_MQTT_CONTROL` | — | `false` | Allow Home Assistant to control the output and setpoints (otherwise read-only) |
| `DPS_MQTT_TOPIC_PREFIX` | — | `dps150` | Prefix for the MQTT state/command topics |
| `DPS_MQTT_DISCOVERY_PREFIX` | — | `homeassistant` | Home Assistant MQTT Discovery prefix |

Unknown flags and stray arguments abort startup — a typo never silently falls
back to the emulator.

## Architecture

```
serial:// ─┐
tcp://   ──┤ transport ── device driver ── hub ──┬── REST API + WebSocket
mock://  ─┘  (reconnect)   (single owner)        ├── history writer ── storage
                                                 ├── auto-stop rules      (SQLite / PostgreSQL)
                                                 └── Telegram notifier
```

- **Transports** — the DPS-150 driver runs over a transport interface with
  reconnect semantics. Three implementations, selected by `DPS_TRANSPORT`:
  `serial://` (direct port), `tcp://` (ser2net raw-TCP bridge) and `mock://`
  (a frame-level emulator for CI e2e and hardware-free development).
- **Hub** — the device port is single-client, so the backend is its sole owner.
  All consumers (REST, WebSocket, history, rules) talk to the device through an
  internal hub that also paces writes (the DPS-150 silently drops back-to-back
  commands).
- **Storage** — SQLite (pure-Go, no cgo) for a self-contained local binary, or
  PostgreSQL for a shared deployment. The schema is portable (time as unix
  millis, no dialect-specific functions). Storage is fail-soft.
- **Auth model (ADR-006)** — with `DPS_AUTH_REQUIRED=true` the `/api/*` routes
  accept a request that carries either a valid `Authorization: Bearer <token>`
  (scope `control` for mutations, `read`/`control` for reads) **or** a trusted
  `Remote-User` header. That header is trusted unconditionally, which only holds
  behind a two-host split: the browser UI host runs Authelia forward-auth in
  front of the service, while the script-facing host strips any client-supplied
  `Remote-User` before the request reaches the backend. Token management itself
  is reachable only through the browser (Authelia) path, so a leaked token can
  never mint or revoke further tokens.

See `docs/architecture/design.md` and `docs/architecture/api-contract.md` for
the full design, and `docs/FNIRSI_DPS-150_Protocol.md` for the protocol
reference.

## Home Assistant (MQTT)

Set `DPS_MQTT_BROKER` and the supply appears in Home Assistant automatically via
MQTT Discovery — sensors for voltage, current, power, temperature, input
voltage, charge (Ah) and energy (Wh), plus CC/CV mode, active protection and a
device-link connectivity sensor. State is published to a retained
`dps150/state` JSON topic; an MQTT Last-Will on `dps150/status` marks the
service offline if it drops.

Control is **off by default**. With `DPS_MQTT_CONTROL=true` the integration also
publishes a `switch` for the output and `number` entities for the voltage and
current setpoints. Note that MQTT commands bypass the browser SSO / token auth
(ADR-006) entirely — the broker's own authentication and ACLs are the trust
boundary, and energizing the output over MQTT has no confirmation step. Only
enable control against a broker you own.

```bash
DPS_MQTT_BROKER=tcp://mqtt.example:1883 \
DPS_MQTT_USERNAME=dps150 DPS_MQTT_PASSWORD=… \
DPS_MQTT_CONTROL=true \
  ./backend/bin/dps150-server -transport mock://
```

## Metrics & Grafana

The backend exports Prometheus metrics at `GET /metrics`, including live
telemetry gauges (`dps150_voltage_volts`, `dps150_current_amps`,
`dps150_power_watts`, `dps150_temperature_celsius`, `dps150_output_enabled`, …)
alongside link, protection and command-latency series. Import
[`deploy/grafana/dashboard.json`](deploy/grafana/dashboard.json) into Grafana
(see [`deploy/grafana/README.md`](deploy/grafana/README.md)) for a ready
dashboard.

## Container images

GitHub Actions ([`.github/workflows/docker-publish.yml`](.github/workflows/docker-publish.yml))
builds and publishes both images to Docker Hub on every push to the default
branch (tagged `latest` + the short commit SHA) and on `v*` tags (semver):

```
docker pull <dockerhub-user>/dps150-web-backend
docker pull <dockerhub-user>/dps150-web-frontend
```

Publishing requires two repository secrets — `DOCKERHUB_USERNAME` (also the
image namespace) and `DOCKERHUB_TOKEN` (a Docker Hub access token). Pull
requests build the images without pushing, so the Dockerfiles stay validated
even without credentials.

## Deployment

- **Local / homelab** — the single binary (SQLite + embedded UI), the
  standalone container (`Dockerfile.standalone`, SQLite) or
  `docker-compose.yml` (backend + frontend + PostgreSQL).
- **Kubernetes** — a Helm chart deployed via ArgoCD (GitOps, ADR-005). The
  chart is a single-replica `Recreate` backend (the device port is
  single-client) behind an nginx frontend and an Ingress; the browser host sits
  behind forward-auth SSO. The chart lives in a **separate platform
  repository**, not here, and releases are image-tag bumps there.

## Development

```bash
make lint    # gofmt + go vet + golangci-lint, oxlint + tsc -b
make test    # go test + vitest
make build   # backend binary (frontend embedded) + frontend bundle
make run     # run the backend on :8080 (emulator by default)
```

End-to-end tests drive the dashboard in Chromium against the real backend
running the emulator — no hardware required. Playwright starts both servers
itself:

```bash
cd backend && go build -o bin/dps150-server ./cmd/server
cd frontend
npx playwright install chromium   # once
npm run e2e
```

The repository layout:

| Path | Description |
|---|---|
| `backend/` | Go backend: device driver, REST API, WebSocket, storage, notifier |
| `frontend/` | React 19 SPA (TypeScript, Vite, Ant Design, TanStack Query, uPlot) |
| `docs/` | Design docs, API contract, runbooks, vendored protocol reference |

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## Credits

The DPS-150 protocol was reverse-engineered by the community:

- Protocol reference: [cho45/fnirsi-dps-150](https://github.com/cho45/fnirsi-dps-150) (MIT) — vendored as `docs/FNIRSI_DPS-150_Protocol.md`
- Original CLI tool: [svenk123/dps150tool](https://github.com/svenk123/dps150tool) (MIT)

FNIRSI and DPS-150 are trademarks of their respective owners. This project is
not affiliated with or endorsed by FNIRSI.

## License

[MIT](LICENSE) © 2026 dps150-web contributors
