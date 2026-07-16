# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- UX/accessibility pass (skill-guided audit): chart series colors are now
  theme-aware — the voltage/current/power/temperature traces use darker,
  higher-contrast variants on the light theme instead of washing out on white;
  `<html lang>` follows the selected UI language (was stuck at `en` while RU is
  primary); the mobile 16px-input rule now also covers text inputs and selects
  (no more iOS zoom-on-focus on names/filters); the output and notification
  Switches gained accessible names; in-table action buttons meet the 44px touch
  target on phones; `ProtectionsPanel` uses the AntD error token instead of a
  hard-coded red; the charge-profiles table rounds `capacityMah`; and the routed
  page content is wrapped in the ErrorBoundary so a render error on any page no
  longer white-screens the whole app.
- Responsive header/navigation: the top bar no longer wraps its nav tabs onto a
  second row at intermediate widths. It is now a single non-wrapping row where
  the horizontal menu fills the middle and folds overflowing items into a "…"
  dropdown (AntD overflow) as the window narrows; below 768px the nav collapses
  to the burger + Drawer as before, and the language/theme controls move into
  the Drawer footer so the mobile header stays on one line (the title
  ellipsizes rather than pushing the connection status off-screen). Verified
  320–1440px with no horizontal overflow.
- Home Assistant MQTT discovery reliability (F-021): discovery configs are now
  published synchronously (waiting for the broker ack) with a connect log for
  visibility, and the publisher subscribes to the HA birth topic
  (`<discovery_prefix>/status`) and re-announces discovery + state whenever
  Home Assistant comes back `online` — so the supply's entities appear
  reliably and survive a Home Assistant restart.
- UI/UX audit fixes (skill-guided, DA-reviewed): uPlot chart axes/gridlines
  now use the active Ant Design theme tokens so they stay legible in dark
  mode (charts remount on theme/locale change without a blank frame);
  event-marker click now deep-links `/events?kind=…` and the Events page
  seeds its kind filter from the query param (previously the link was
  ignored); mobile setpoint/protection inputs use 16px text + 44px targets
  (no iOS focus-zoom); CC/CV mode is shown as a single active tag (no longer
  color-only); data-load error alerts gained a retry action; a dashed
  temperature series, chart aria-label summaries, and a legibility bump to
  idle metering readouts.
- Auto-stop rules engine (F-018), adversarial-review fix: for `scope: always`,
  an output-off gap no longer counts toward the `currentBelow` hysteresis
  window — the engine now excludes the off period's duration from the
  counted hold time instead of treating it as elapsed wall clock, so an
  interruption at least as long as `forSeconds` can no longer, by itself,
  fire the rule unconditionally on the very first telemetry tick after the
  output comes back on; genuine progress made before and after the gap
  still accumulates correctly.
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
- `docker-compose.yml` now pulls the prebuilt backend/frontend images from
  Docker Hub (`docker compose pull`) instead of building from source.
- CI: GitHub Actions bumped to Node.js 24 runtimes (`actions/checkout@v5`,
  `docker/*` v4/v6/v7) to clear the Node.js 20 deprecation warning.

### Added
- Battery charging mode (F-023, ADR-008, `internal/charger`): a backend-supervised
  charge engine that turns the supply into a proper CC-CV charger for **Li-ion,
  LiFePO4 and lead-acid** packs. A charge is an ordered list of phases
  (precharge → CC/CV → Pb float) compiled from a per-chemistry preset; the
  hardware does the CC/CV regulation while the engine watches telemetry and
  terminates on the current tapering below the cutoff. New page **"Charge"** with
  saved profiles, a live view (V+I chart with phase bands, phase/elapsed/ETA,
  mAh delivered, safety-cap progress bars) and session history. REST
  `/charge/profiles*`, `/charge/preflight`, `/charge/profiles/{id}/start`,
  `/charge/stop`, `/charge/active`, `/charge/sessions*`; WS `chargeProgress`/
  `chargeSession`; terminal Telegram summary. Strict, non-disable-able safety
  envelope: a **pre-flight** measures the pack voltage with the output off and
  refuses a reversed/absent battery or a cell-count mismatch; the start order
  writes the protections and `Vset` **before** energizing (reverse-current
  guard); a **telemetry-staleness watchdog** cuts the output if ticks stop (the
  device hanging over ser2net does not surface as a link-loss); an absolute
  per-chemistry voltage ceiling, a capacity cap and a hard timeout abort the
  charge; a device Ah-counter reset is treated as a fault; and a shared
  single-owner interlock makes a charge and a sequence mutually exclusive
  (`409 charge_active`/`sequence_active`). Multi-cell lithium requires attesting
  an external BMS. NiMH is intentionally deferred (no autonomous termination and
  no battery-temperature sensor make it unsafe to leave unattended on this rig).
- Russian README (`README.ru.md`) alongside the English one, cross-linked at
  the top of each; the screenshots were refreshed in the light theme and now
  include the Sequences page and its step-tree editor.
- Programmable sequences (F-022, `internal/sequence`): test programs built
  from a tree of steps — `setHold` (set V/I, advance when a condition holds),
  `ramp` (linearly sweep V or I over time) and nestable `loop` blocks — run by
  a telemetry-driven interpreter over the device hub. Advance conditions reuse
  the auto-stop condition model (`currentBelow`/`capacityAbove`/`energyAbove`/
  `elapsedAbove`). One run at a time: starting a run turns the output on and
  every terminal path (completion, stop, protection trip, backend shutdown,
  error) turns it off; manual device/profile mutations return `409
  sequence_active` while a run is active, and user auto-stop rules are
  suspended for its duration. REST CRUD + `POST /sequences/:id/run|stop` +
  `GET /sequences/active`; live step progress over the WebSocket
  `sequenceProgress` event.
- Home Assistant integration over MQTT Discovery (F-021, `internal/mqtt`):
  a retained JSON state topic, an availability topic with an MQTT Last-Will,
  and auto-published discovery configs so the supply shows up in HA as
  sensors (V/I/P, temperature, input voltage, Ah/Wh, CC-CV mode, active
  protection, device-link connectivity). Control is opt-in via
  `DPS_MQTT_CONTROL` (default off): when enabled, an output `switch` and
  voltage/current `number` entities become available. MQTT commands bypass the
  Authelia/token gate by design — the broker's own auth/ACLs are the trust
  boundary (ADR-007).
- Prometheus telemetry gauges (F-021): `dps150_voltage_volts`,
  `dps150_current_amps`, `dps150_power_watts`, `dps150_temperature_celsius`,
  `dps150_input_voltage_volts`, `dps150_capacity_amp_hours`,
  `dps150_energy_watt_hours`, `dps150_output_enabled`, and the voltage/current
  setpoint gauges — plus `deploy/grafana/dashboard.json`, a ready Grafana
  dashboard over them and the existing link/protection/latency series.
- README screenshots (dashboard, history, automation, profiles, events,
  settings) captured from the running app with the device emulator.
- GitHub Actions workflow that builds and publishes the backend and
  frontend images to Docker Hub on push/tag (`.github/workflows/docker-publish.yml`).
- Standalone all-in-one Docker image (`deploy/docker/Dockerfile.standalone`):
  a single container serving the web UI and REST API from one binary with
  SQLite storage — no PostgreSQL required.
- Open-source cleanup: removed process/tooling scaffolding not part of the
  project (`.claude/`, `CLAUDE.md`, `.serena/`, boilerplate process docs,
  helper scripts and issue/MR templates); added a header language switcher
  (RU/EN, persisted) and routed the last hardcoded string in the tokens
  hint through i18n.
- Test hardening (TD-004): storage-ready waits in backend test helpers
  raised from 5s to 20s to stop flakes when the shared CI runner is
  contended.
- Open-source preparation (D-001): MIT `LICENSE`, an English `README.md`,
  a `CONTRIBUTING.md`, and genericized deployment specifics — private
  domains replaced with `example.com` placeholders in docs and godoc, and
  the ser2net runbook's device serial replaced with a `XXXXXXXX`
  placeholder — ahead of publishing a public GitHub mirror; the remaining
  project documentation (design doc, API contract, runbooks, LikeC4 views)
  was translated from Russian to English.
- CSV export UI (F-019): "Export CSV" buttons on the History and Events
  pages (`src/api/export.ts`) build the `GET /api/v1/history.csv`
  (current viewed `[from, to]` and `resolution=auto`) / `GET
  /api/v1/events.csv` (current kind filter plus a dedicated export date
  range, defaulting to the last 24 h) URLs and hand them to the browser
  via a transient `<a download>` element — the backend's own
  `Content-Disposition: attachment` does the rest, so there is no
  fetch/blob buffering of the exported file in page memory.
- Automation UI (F-018): `/automation` now has the full auto-stop rules
  manager instead of the route placeholder — a rules table (name,
  human-readable condition, action, scope, an `enabled` switch and last
  triggered time) with a create/edit constructor modal for the four
  condition types (`currentBelow{amps,forSeconds}` / `capacityAbove{ah}`
  / `energyAbove{wh}` / `elapsedAbove{seconds}`), full CRUD against
  `GET/POST/PUT/DELETE /api/v1/automation/rules(/{id})`; a paginated
  trigger history table (`GET /api/v1/automation/triggers`) that
  refreshes live off the WS `event` kind `autoStop`; a persistent
  disclaimer that rules run in the cluster and auto-stop is not
  guaranteed while the device link is down (hardware protections are the
  fallback); and a `storage_unavailable` Alert for the 503 case.
- API tokens UI (F-020): a Settings page "API tokens" section (`GET/POST/DELETE /api/v1/tokens`) lists each token's name/scope/created/last-used, creates one via a name + read/control scope modal, and shows the freshly minted bearer secret exactly once in a dismiss-only dialog (no mask/Escape close) with a copy button — closing it clears the secret from state for good, matching the backend never returning it again; delete requires a Popconfirm; a persistent Alert covers 503 `storage_unavailable`; the section's hint names the scripted-access host by deriving `dps150-api.<domain>` from the page's own origin (falling back to a generic pattern) rather than hardcoding a real domain, and points out the browser UI itself goes through SSO.
- API tokens and bearer/forward-auth gate (F-020, ADR-006): `api_tokens(id, name, scope, token_hash, created_at, last_used_at)` storing only the SHA-256 hash of a `dps_<base64url>` secret shown once at creation; `GET/POST /api/v1/tokens` and `DELETE /api/v1/tokens/{id}` for management, reachable only through the browser UI behind Authelia (`Remote-User`), never by a bearer token even scope `control`; a real `authGate` on `/api/v1/*` now requires either a valid `Authorization: Bearer <token>` with sufficient scope (`control` for mutations, `read` or above for reads) or a trusted `Remote-User` header, otherwise 401 `unauthorized` (403 `forbidden` for insufficient scope); a down or unconfigured token store fails a bearer attempt closed (401), never 503 — a database outage must never bypass auth. `/healthz` and `/metrics` stay outside the gate.
- Auto-stop rules engine and API (F-018): `automation_rules`/`automation_triggers` tables (condition stored as JSON); CRUD at `GET/POST/PUT/DELETE /api/v1/automation/rules(/{id})` (404 `rule_not_found`) plus `GET /api/v1/automation/triggers` (paginated firing history), all 503 `storage_unavailable` while the database is down; a hub-subscribing `internal/automation` engine evaluates enabled rules against the live telemetry stream (`currentBelow`/`capacityAbove`/`energyAbove`/`elapsedAbove`) with duration/hysteresis (a single telemetry spike never fires a rule — the condition must hold for `forSeconds`), `scope: session` resetting a rule's progress when the output turns off vs. `scope: always` carrying it across on/off cycles; on firing it switches the output off, journals an `autoStop` entry (mirrored to WS `event`), records the trigger history and optionally notifies via the existing Telegram sender; every rule is suspended (not evaluated, no accumulated progress) while the device link is down.
- CSV export (F-019): streaming `GET /api/v1/history.csv` (columns
  `timestamp,voltage,current,power,temperature,output_on` for `resolution=raw`,
  `timestamp,v_min,v_avg,v_max,i_min,i_avg,i_max,p_min,p_avg,p_max,t_avg,samples`
  for `1m`) and `GET /api/v1/events.csv` (`timestamp,kind,data`, oldest-first,
  filterable by `kind`), both with `Content-Disposition: attachment` and
  ISO 8601 UTC timestamps. Range validation matches `GET /api/v1/history`
  (`400 invalid_range`) but without the 20000-point cap: rows are read from
  the store in fixed-size pages and flushed to the response as they arrive,
  so an export never buffers the whole range in memory. `503
  storage_unavailable` while the database is down.
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
- Telegram notifications and metering sessions (F-015, F-017): a hub-fed notifier pushes protection trips, device link loss/recovery, output switching and metering-session summaries to the Telegram Bot API (credentials only from `DPS_TELEGRAM_TOKEN`/`DPS_TELEGRAM_CHAT_ID`, per-type 30 s cooldown with "repeated N times" aggregation, bounded non-blocking queue), `GET/PUT /api/v1/settings/notifications` persisted in the settings KV (`configured: false` when the env is empty), and a `meteringSession {capacityAh, energyWh, durationMs}` journal event recorded as the counter delta of each output-on..off session.
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
- Helm chart and prod auto-deploy (F-009): `deploy/helm/dps150-web` with a single-replica Recreate backend (single-client device), nginx frontend, path-based Ingress `dps150.example.com` behind Authelia with a dedicated cert-manager certificate, Vault-sourced DB/registry credentials via VSO (fail-soft on first deploy), and an auto `deploy:prod` CI job (master → ns `dps150`, image tag `$CI_COMMIT_SHORT_SHA`).
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
  GitLab issue tracker seeded (milestones "Stage 1 PoC" / "Stage 2 MVP" / "Stage 3 v1.0").
- Monorepo scaffold (F-001): Go backend skeleton (Gin, `/healthz`, env config,
  graceful shutdown, slog), React frontend (Vite, TypeScript, Ant Design,
  TanStack Query, i18n ru/en, vitest smoke test), `Makefile`
  (build/lint/test/run), `.editorconfig`, English README.
- GitLab CI pipeline (F-008): lint/test/build stages (gofmt + go vet +
  golangci-lint, oxlint + tsc, commitlint, changelog gate, `go test -cover`,
  vitest), two-tier Go/npm caches, buildx-over-dind Docker image builds
  (`deploy/docker/Dockerfile.backend`, `deploy/docker/Dockerfile.frontend`)
  pushing `:<short-sha>` and `:latest` to the registry on master.
