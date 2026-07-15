# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Charts (F-013), adversarial-review fixes: `EventMarkers`' collision
  resolution is now a proper two-pass sweep instead of nudging each
  marker around its own raw position — the old approach could never
  separate markers whose raw position clamped onto the same off-canvas
  edge (e.g. events newer than the last flushed sample), leaving them
  fully occluding each other; and the History page's "Month" quick
  range now requests 13 days instead of a full 30, since the backend
  caps `resolution=1m` responses at 20000 minute-points (1m already
  being the coarsest tier) — a genuine 30-day span was guaranteed to
  400 `range_too_dense` on any deployment with more than ~14 days of
  continuous history.
- `ProtectionsPanel` (F-014-UI): client-side validation now rejects 0 for OVP/OCP/OPP/OTP (only LVP may be 0), matching the backend's `> 0` contract and the sibling `ProfileFormModal` validator — previously a 0 threshold passed inline validation and only got rejected by the server's 400 `invalid_protection`.

### Changed
- Deploys moved to GitOps: the Helm chart lives in argocd-platform
  (`apps/dps150`), releases are image-tag bumps there; the CI deploy stage
  is gone (ADR-005 supersedes the ADR-003 deploy mechanism).

### Added
- API tokens and bearer/forward-auth gate (F-020, ADR-006): `api_tokens(id, name, scope, token_hash, created_at, last_used_at)` storing only the SHA-256 hash of a `dps_<base64url>` secret shown once at creation; `GET/POST /api/v1/tokens` and `DELETE /api/v1/tokens/{id}` for management, reachable only through the browser UI behind Authelia (`Remote-User`), never by a bearer token even scope `control`; a real `authGate` on `/api/v1/*` now requires either a valid `Authorization: Bearer <token>` with sufficient scope (`control` for mutations, `read` or above for reads) or a trusted `Remote-User` header, otherwise 401 `unauthorized` (403 `forbidden` for insufficient scope); a down or unconfigured token store fails a bearer attempt closed (401), never 503 — a database outage must never bypass auth. `/healthz` and `/metrics` stay outside the gate.
- Charts (F-013): a Dashboard `LiveChart` (uPlot) streaming a 5/15/30 min
  sliding V/I/P window straight from the live WS state (no extra network
  traffic), paused while the tab is hidden; a `HistoryPage` with
  hour/day/week/month presets plus a custom range picker, drag-to-zoom
  (double-click to reset) against `GET /api/v1/history?resolution=auto`
  that transparently drills into raw samples as the zoomed span narrows,
  a min..max band around the average at `1m` resolution, per-quantity
  V/I/P/T show/hide toggles, an exact-value/time cursor legend, and
  `GET /api/v1/events` markers (clickable through to `/events` with a
  time filter).
- Profiles, presets, protections and event journal UI (F-010/F-011/F-014): `ProfilesPage` (CRUD modal with per-protection range hints, apply-to-device via Popconfirm that never touches the output relay, assign-to-M1–M6 dropdown, live presets grid), dashboard `QuickProfiles` (one-click apply, applied-feedback, disabled offline) and `ProtectionsPanel` (inline OVP/OCP/OPP/OTP/LVP editing with a highlighted tripped-threshold row) slots, and `EventsPage` (paginated, kind-filterable journal with localized timestamps, an expandable raw-JSON row and WS-driven live refresh); 503 `storage_unavailable` renders a page Alert, 409 `device_offline` a toast.
- Notification settings UI, metering card and mobile/dark-mode layout (F-015, F-016, F-017): `SettingsPage` for `GET/PUT /api/v1/settings/notifications` (Telegram + 4 event-type switches, disabled with an alert when Telegram env is unset, per-switch save feedback); `MeteringCard` dashboard slot showing live capacity/energy and the current session duration (muted while the output is off), plus the last finished session from `meteringSession` journal events (WS live, REST-seeded on load); a burger/Drawer nav and AntD dark/light theme (system preference + header toggle, persisted) below ~640px, with large touch targets and a compact secondary-readings row on the dashboard.
- Stage-2 integration polish: meteringSession and profileApplied journal
  kinds are mirrored to WebSocket clients, profile apply answers 409 while
  the device is offline, and the initial device connect no longer sends a
  "link restored" Telegram notification.
- Prometheus metrics (TD-001): `GET /metrics` (promhttp, outside `/api/v1` and not exposed through the Ingress) with domain series — device link and reconnects, protection state by enum, hub command duration histogram, WS clients, storage readiness, dropped updates — wired non-invasively (hub subscriber + command wrapper), plus a Helm `ServiceMonitor` on the backend Service (30s interval, enabled by default via `serviceMonitor.*` values).
- Telegram notifications and metering sessions (F-015, F-017): a hub-fed notifier pushes protection trips, device link loss/recovery, output switching and metering-session summaries to the Telegram Bot API (credentials only from `DPS_TELEGRAM_TOKEN`/`DPS_TELEGRAM_CHAT_ID`, per-type 30 s cooldown with "повторилось N раз" aggregation, bounded non-blocking queue), `GET/PUT /api/v1/settings/notifications` persisted in the settings KV (`configured: false` when the env is empty), and a `meteringSession {capacityAh, energyWh, durationMs}` journal event recorded as the counter delta of each output-on..off session.
- Protection limits API and device event journal (F-014): `PUT /api/v1/device/protections` accepts any subset of ovp/ocp/opp/otp/lvp, validates the contract bounds and float32 representability (the device wire format; values like lvp=1e39 are rejected instead of reaching the wire as +Inf), returns all five effective values, journals a `protectionsChanged` entry and mirrors it onto the WS `event` stream; a hub-subscriber journal service records `protectionTrip` (with a V/I/P snapshot at the moment of the trip), `deviceConnected`/`deviceDisconnected` and `outputOn`/`outputOff` into the `events` table (fail-soft: a dead database only drops entries with a rate-limited warning, never blocks the hub); `GET /api/v1/events` serves the journal newest-first with kind/time filters and limit/offset paging (default 50, max 500) plus the unpaged total.
- Protection limits API and device event journal (F-014): `PUT /api/v1/device/protections` accepts any subset of ovp/ocp/opp/otp/lvp, validates the contract bounds, returns all five effective values and journals a `protectionsChanged` entry; a hub-subscriber journal service records `protectionTrip` (with a V/I/P snapshot at the moment of the trip), `deviceConnected`/`deviceDisconnected` and `outputOn`/`outputOff` into the `events` table (fail-soft: a dead database only drops entries with a rate-limited warning, never blocks the hub); `GET /api/v1/events` serves the journal newest-first with kind/time filters and limit/offset paging (default 50, max 500) plus the unpaged total.
- Profiles and hardware presets API (F-010, F-011): `profiles` table with
  CRUD at `/api/v1/profiles` (name unique, device-envelope validation) and
  `POST /profiles/{id}/apply` writing the setpoints plus the full protection
  set — never the output relay — and journaling `profileApplied` (fail-soft);
  hardware preset slots M1–M6 via `GET/PUT /api/v1/device/presets` (by
  profileId or explicit voltage+current, V+I only) fed from the cached dump.
- Telemetry history (F-012): 2 Hz samples batched into storage every 5 s (loss-tolerant — drops with a throttled warn while the DB is down), hourly minute aggregation catching up after downtime plus retention jobs (raw 30 days, 1m aggregates 365 days), and `GET /api/v1/history` with raw/1m/auto resolutions capped at 20000 points per the API contract v2.
- App shell for stage 2: client-side routing (react-router-dom v7), AntD
  layout with top navigation (Dashboard / History / Profiles / Events /
  Settings) and the device link badge in the header, dashboard moved to
  `src/pages/DashboardPage.tsx` with `slot:*` anchors for the parallel
  tracks, i18n'd stub pages and navigation smoke tests.
- Registry pull-secret runbook (`docs/runbooks/registry-pull-secret.md`): the Vault path `secret/dps150/registry` is a hard deploy prerequisite — seeded with a `read_registry` deploy token and verified live (VSS synced, test pod pulled an image) so the first `deploy:prod` does not hit ImagePullBackOff.
- Helm chart and prod auto-deploy (F-009): `deploy/helm/dps150-web` with a single-replica Recreate backend (single-client device), nginx frontend, path-based Ingress `dps150.r2bnj.ru` behind Authelia with a dedicated cert-manager certificate, Vault-sourced DB/registry credentials via VSO (fail-soft on first deploy), and an auto `deploy:prod` CI job (master → ns `dps150`, image tag `$CI_COMMIT_SHORT_SHA`).
- Storage layer (F-007): GORM over SQLite (pure-Go, no cgo) or PostgreSQL
  selected by `DPS_DB_DRIVER`/`DPS_DB_DSN`, CLI flags mirroring every
  `DPS_*` variable (`-transport`, `-listen`, `-log-level`, `-db-driver`,
  `-db-dsn`; flags win, unknown flags abort startup instead of silently
  running the emulator), fail-soft background reconnect
  with backoff (app runs and controls the device with the DB down),
  `settings` foundation model with Get/Set and AutoMigrate hooks for
  feature models; single-binary serving of the embedded frontend bundle
  (`go:embed`, `make build-backend`/`make release-binaries` for
  darwin/arm64 + linux/amd64 + linux/arm64) and a root `docker-compose.yml`
  (backend + nginx frontend with WS-aware `/api` proxy + postgres:17,
  `serial` profile for real hardware).
- Playwright e2e tests for the dashboard against the real backend with the
  built-in device emulator (`frontend/e2e/`, `npm run e2e`, CI job `e2e`):
  live telemetry over WS, setpoint apply, confirmed output toggle with the
  emulator load model, client-side limit validation.
- Stage-1 integration: `mock://` wires the built-in emulator in `main.go`,
  hub paces device writes (50 ms gap; real DPS-150 hardware silently drops
  back-to-back frames — discovered live, documented in the ser2net runbook),
  ser2net bridge installed on pve and verified against the physical PSU
  end-to-end (I-001, `docs/runbooks/ser2net-pve.md`).
- Live dashboard (F-006): single-page React UI with WebSocket telemetry
  (reconnect + backoff), large V/I/P readings, CC/CV and protection
  indicators, setpoints form and confirmed output switch via REST
  (TanStack Query), event/link toasts, ru/en i18n, Vite dev proxy to the
  backend, vitest coverage for the reducer, form, switch and page smoke.
- Backend core (F-005): device hub (reconnect loop with exponential backoff,
  state cache, subscriber fan-out with drop policy, serialized time-bounded
  writes, connected reported only once the device answers a full dump),
  REST API (`GET /api/v1/device`, `PUT /api/v1/device/setpoints`,
  `PUT /api/v1/device/output`) and WebSocket `/api/v1/ws` streaming
  state/telemetry/status/event messages per the frozen API contract.
- In-memory DPS-150 device emulator (F-004): a `transport.Dialer` speaking the
  real frame protocol — session gating, periodic telemetry, register writes
  with RX echo, resistive load model with CC/CV transitions, latching
  protection trips, Ah/Wh metering and full-dump reads
  (`backend/internal/device/emulator`).
- Device transports (F-003): `serial://` (go.bug.st/serial, 115200 8N1,
  optional `?baud=N`) and `tcp://` (ser2net, 5s dial timeout, keepalive)
  dialers behind the `transport.Dialer` interface with context-aware Dial
  and Close-unblocks-Read semantics (`backend/internal/transport`).
- DPS-150 protocol library (F-002): frame codec with checksum, typed register
  map, TX encoding helpers, streaming RX parser with resynchronization and
  typed event decoding (`backend/internal/device/protocol`, stdlib-only).
- Project bootstrap: process boilerplate (lite profile), design doc, ADR-001..004,
  GitLab issue tracker seeded (milestones «Этап 1 PoC» / «Этап 2 MVP» / «Этап 3 v1.0»).
- Monorepo scaffold (F-001): Go backend skeleton (Gin, `/healthz`, env config,
  graceful shutdown, slog), React frontend (Vite, TypeScript, Ant Design,
  TanStack Query, i18n ru/en, vitest smoke test), `Makefile`
  (build/lint/test/run), `.editorconfig`, English README.
- GitLab CI pipeline (F-008): lint/test/build stages (gofmt + go vet +
  golangci-lint, oxlint + tsc, commitlint, changelog gate, `go test -cover`,
  vitest), two-tier Go/npm caches, buildx-over-dind Docker image builds
  (`deploy/docker/Dockerfile.backend`, `deploy/docker/Dockerfile.frontend`)
  pushing `:<short-sha>` and `:latest` to the registry on master.
