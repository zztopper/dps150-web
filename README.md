# dps150-web

Web-based control panel for the **FNIRSI DPS-150** programmable DC power supply
(0–30 V / 0–5 A / 150 W, USB-CDC serial).

Features (see `docs/architecture/design.md` for the full design):

- Full control of the power supply: voltage/current setpoints, output switch,
  protection limits (OVP/OCP/OPP/OTP/LVP), hardware presets M1–M6
- Named profiles (V + I + protections) stored in a database
- Live telemetry (2 Hz) and historical charts with one month of retention
- Runs locally next to the device or in Kubernetes behind a serial-over-TCP
  bridge (ser2net)

## Repository layout

| Path | Description |
|---|---|
| `backend/` | Go backend: device driver, REST API, WebSocket, storage |
| `frontend/` | React SPA (TypeScript, Vite, Ant Design, TanStack Query) |
| `docs/` | Design doc, vendored DPS-150 protocol reference, process docs |

## Requirements

- Go 1.25+
- Node.js 20+
- golangci-lint (for `make lint`)

## Local development

```bash
make build          # build backend binary + frontend bundle
make lint           # gofmt + go vet + golangci-lint, oxlint + tsc
make test           # go test + vitest
make run            # run backend on :8080 (device emulator by default)
make run-frontend   # run Vite dev server
```

The backend is configured via command-line flags or environment variables
(a flag wins over its variable):

| Flag | Variable | Default | Description |
|---|---|---|---|
| `-listen` | `DPS_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `-transport` | `DPS_TRANSPORT` | `mock://` | Device transport: `serial:///dev/ttyUSB0`, `tcp://host:port` or `mock://` |
| `-log-level` | `DPS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `-db-driver` | `DPS_DB_DRIVER` | `sqlite` | Storage backend: `sqlite` or `postgres` |
| `-db-dsn` | `DPS_DB_DSN` | `dps150.db` | File path for sqlite, `postgres://user:pass@host:port/db` for postgres |

Unknown flags and stray arguments abort startup — a typo never silently
falls back to the emulator. With `mock://` the backend talks to a built-in
DPS-150 emulator, so no hardware is required for development:

```bash
./dps150-server -transport serial:///dev/tty.usbmodem101   # real hardware
./dps150-server                                            # emulator
```

## End-to-end tests

Playwright drives the dashboard in Chromium against the real backend running
the built-in device emulator — no hardware required:

```bash
cd backend && go build -o bin/dps150-server ./cmd/server   # once per change
cd frontend
npx playwright install chromium                            # once
npm run e2e
```

Playwright starts both servers itself: the backend with `DPS_TRANSPORT=mock://`
on a dedicated port (default `18080`, override with `E2E_BACKEND_PORT`) and the
Vite dev server on `:5173` proxying `/api` to it. The HTML report lands in
`frontend/playwright-report/`.

## Credits

- Protocol reverse engineering: [cho45/fnirsi-dps-150](https://github.com/cho45/fnirsi-dps-150) (MIT)
- Original CLI tool: [svenk123/dps150tool](https://github.com/svenk123/dps150tool) (MIT)
