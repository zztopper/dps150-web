# API contract: backend ↔ frontend (v1)

Fixed for parallel development of F-005 (backend) and F-006 (frontend).
Contract changes — only by editing this file in the same MR.

General rules: JSON, camelCase, units — volts/amperes/watts/°C/Ah/Wh,
timestamps — unix millis (number). Errors:
`{"error": {"code": "<machine_code>", "message": "<human text>"}}` +
an appropriate HTTP status (400 validation, 409 device offline, 500 other).

## REST

### GET /healthz
`200 {"status":"ok"}` — liveness, no side effects.

### GET /api/v1/device
Current device state (from the hub cache, does not wait for the device):

```json
{
  "connected": true,
  "transport": "tcp://10.20.0.5:2150",
  "info": {"model": "DPS-150", "hardware": "V1.0", "firmware": "V1.1"},
  "state": {
    "outputOn": false,
    "mode": "cv",
    "protection": "ok",
    "setpoints": {"voltage": 12.0, "current": 1.0},
    "measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
    "inputVoltage": 20.0,
    "temperature": 31.5,
    "limits": {"maxVoltage": 19.8, "maxCurrent": 5.1},
    "metering": {"capacityAh": 0.0, "energyWh": 0.0},
    "protections": {"ovp": 31.0, "ocp": 5.2, "opp": 155.0, "otp": 75.0, "lvp": 4.5},
    "brightness": 10,
    "volume": 5,
    "updatedAt": 1784000000000
  }
}
```

- `mode`: `"cc" | "cv"`; `protection`: `"ok" | "ovp" | "ocp" | "opp" | "otp" | "lvp" | "rep"`.
- `info` and `state` may be `null` while the device has never responded
  (`connected: false` is then mandatory).

### PUT /api/v1/device/setpoints
Body: `{"voltage": 12.0}` / `{"current": 0.5}` / both fields. Values
are validated against `limits` (fallback 30.0 V / 5.0 A): out of range —
`400 invalid_setpoint`. Device offline — `409 device_offline`.
Response `200`: `{"voltage": 12.0, "current": 0.5}` — the applied setpoints.

### PUT /api/v1/device/output
Body: `{"on": true}` or `{"on": false}`. Confirmation of enabling is the
UI's responsibility, the API requires no additional fields. Offline — `409 device_offline`.
Response `200`: `{"on": true}`.

Reserved for Stage 2 (NOT implemented in F-005/F-006):
`PUT /api/v1/device/protections`, `GET/POST /api/v1/profiles`,
`GET /api/v1/history`, `GET /api/v1/events`.

## WebSocket: GET /api/v1/ws

Server→client only. Message format: `{"type": "...", "data": {...}}`.

| type | when | data |
|---|---|---|
| `state` | immediately after the client connects; after every successful setpoint/output write | full object as in `GET /api/v1/device` |
| `telemetry` | every device telemetry packet (~2 Hz) | see below |
| `status` | on a change of the device connection | `{"connected": false, "transport": "..."}` |
| `event` | protection trip, CC/CV change, output on/off | `{"kind": "protectionTrip"\|"modeChange"\|"outputChange", "protection": "ovp", "mode": "cc", "outputOn": true, "ts": 1784000000000}` — fields depend on kind |

`telemetry.data`:

```json
{
  "measured": {"voltage": 11.99, "current": 0.5, "power": 6.0},
  "inputVoltage": 20.0,
  "temperature": 31.5,
  "mode": "cv",
  "protection": "ok",
  "outputOn": true,
  "metering": {"capacityAh": 0.001, "energyWh": 0.02},
  "ts": 1784000000000
}
```

The client must survive WS drops (reconnect with backoff) and, after a
reconnect, rebuild state from the first `state` message.
Unknown `type`s are silently ignored by the client (forward-compat).

---

# API contract v2: Stage 2 (MVP)

Fixed for parallel development of F-010..F-017 + TD-001.
The v1 general rules apply (camelCase, unix millis, error format).
New error code: `storage_unavailable` (503) — DB unavailable (fail-soft).

## Profiles (F-010)

`Profile`: `{"id": 1, "name": "3.3V logic", "voltage": 3.3, "current": 0.5,
"protections": {"ovp": 3.6, "ocp": 0.6, "opp": 10.0, "otp": 75.0, "lvp": 4.5},
"createdAt": <ms>, "updatedAt": <ms>}`

- `GET /api/v1/profiles` → `{"items": [Profile...]}` (sorted by name)
- `POST /api/v1/profiles` (without id) → `201 Profile`; name unique → `409 profile_name_taken`
- `PUT /api/v1/profiles/{id}` → `200 Profile`; `404 profile_not_found`
- `DELETE /api/v1/profiles/{id}` → `204`
- `POST /api/v1/profiles/{id}/apply` → `200 {"applied": true}` — writes to
  the device C1/C2 + D1–D5 and confirms by reading back. INVARIANT: the output is NOT
  enabled and NOT disabled. Offline → `409 device_offline`.

## Hardware presets M1–M6 (F-011)

- `GET /api/v1/device/presets` → `{"items": [{"slot": 1, "voltage": 5.0, "current": 1.0}, ...]}`
  (6 slots, from the FF-dump cache)
- `PUT /api/v1/device/presets/{slot}` body `{"profileId": 1}` OR
  `{"voltage": 5.0, "current": 1.0}` → `200 {"slot": 1, "voltage": 5.0, "current": 1.0}`.
  Only V+I go into the slot (the hardware does not store protections in presets).
  slot outside 1..6 → `400 invalid_slot`.

## Protection setpoints (F-014)

- `PUT /api/v1/device/protections` body `{"ovp"?: 31.0, "ocp"?: 5.2, "opp"?: 155.0,
  "otp"?: 75.0, "lvp"?: 4.5}` (any subset) → `200` with the applied
  values of all five. Validation: > 0, ovp ≤ 31, ocp ≤ 5.2, opp ≤ 155,
  otp ≤ 80, lvp ≥ 0 → otherwise `400 invalid_protection`. The device wire
  format is float32: values not representable by a finite float32
  (> ~3.4e38) are rejected with the same `400 invalid_protection`.

## History (F-012)

- `GET /api/v1/history?from=<ms>&to=<ms>&resolution=raw|1m|auto` →
  ```json
  {"resolution": "raw", "items": [
    {"ts": 1784..., "voltage": 12.0, "current": 0.5, "power": 6.0,
     "temperature": 31.5, "outputOn": true}
  ]}
  ```
  For `1m`, instead of instantaneous values — `{"ts", "voltage": {"min","avg","max"},
  "current": {...}, "power": {...}, "temperature": {"avg"}, "samples": 120}`.
  `auto`: raw when (to-from) ≤ 2 h, otherwise 1m. from ≥ to or range > 400 days →
  `400 invalid_range`. The response is capped at 20000 points (`400 range_too_dense` with
  a hint to switch to 1m). DB unavailable → `503 storage_unavailable`.

## Event journal (F-014, written by everyone)

`Event`: `{"id": 1, "ts": <ms>, "kind": "...", "data": {...}}`; kinds:
`protectionTrip {protection, snapshot{voltage,current,power}}`,
`deviceConnected {}`, `deviceDisconnected {}`,
`outputOn {}`, `outputOff {}`,
`profileApplied {profileId, name}`,
`protectionsChanged {ovp,ocp,opp,otp,lvp}`,
`meteringSession {capacityAh, energyWh, durationMs}` (summary when the output is turned off),
`autoStop` (reserved, Stage 3).

- `GET /api/v1/events?from&to&kind&limit=50&offset=0` →
  `{"items": [Event...], "total": 123}` (newest first; kind — CSV filter)

## Notification settings (F-015)

- `GET /api/v1/settings/notifications` / `PUT ...` body/response:
  `{"telegramEnabled": true, "events": {"protectionTrip": true,
  "deviceLink": true, "output": false, "meteringSession": true}}`
  Bot token and chat id — ONLY from env (`DPS_TELEGRAM_TOKEN`,
  `DPS_TELEGRAM_CHAT_ID`; in k8s — from the VaultStaticSecret `secret/apps/dps150-web/telegram`),
  neither read nor written via the API. If the env is empty — `telegramEnabled`
  is ignored, and `GET` additionally returns `"configured": false`.

## WS additions

Journal kinds that have no v1 equivalent on the WS — `protectionsChanged`,
`profileApplied`, `meteringSession` — are relayed as an `event` message (v1):
`data` = the journal record fields + `kind` + `ts`. Link and output transitions
remain v1 `status` messages and `event` kind `outputChange`; the journal
names `deviceConnected`/`deviceDisconnected`/`outputOn`/`outputOff` are not
duplicated on the WS. `telemetry.data.metering` is already in v1. There are no new message types.

## DB schema (all times unix millis, portable SQL)

- `profiles(id PK, name UNIQUE, voltage, current, ovp, ocp, opp, otp, lvp, created_at, updated_at)`
- `samples(ts BIGINT PK, voltage, current, power, input_voltage, temperature, output_on, mode, protection)` — 2 Hz, 30-day retention
- `samples_1m(ts BIGINT PK, v_min, v_avg, v_max, i_min, i_avg, i_max, p_min, p_avg, p_max, t_avg, cnt)` — 365-day retention
- `events(id PK autoincr, ts BIGINT INDEX, kind TEXT INDEX, data TEXT/JSON)`
- `settings(key PK, value)` — already exists (F-007)

Retention and per-minute aggregation — backend background jobs (intervals are constants,
the janitor runs once an hour). Models are wired in via storage.Config.Models (F-007).

## Metrics (TD-001)

`GET /metrics` — Prometheus (promhttp), needs no Authelia bypass
(scraped from inside the cluster via a ServiceMonitor on the backend Service).

## Frontend file structure (so that parallel tracks don't conflict)

The router and page skeleton are laid down in the base branch (react-router-dom,
AntD Layout with a menu). Pages: `src/pages/DashboardPage.tsx` (the existing
dashboard), `HistoryPage.tsx` (F-013), `ProfilesPage.tsx` (F-010/011),
`EventsPage.tsx` (F-014), `SettingsPage.tsx` (F-015). Dashboard slots —
separate component files: `src/components/LiveChart.tsx` (F-013),
`src/components/ProtectionsPanel.tsx` (F-014), `src/components/QuickProfiles.tsx`
(F-010), `src/components/MeteringCard.tsx` (F-017). Each track adds ONLY
its own files + a single line wiring the slot into DashboardPage (the spot is marked
with anchor comments `{/* slot:... */}`). i18n keys — prefixed by page.

---

# API contract v3: Stage 3 (v1.0)

Fixed for parallel development of F-018/F-019/F-020. The v1/v2 rules
apply. Authentication — ADR-006: the browser UI behind Authelia (host
`dps150.example.com`), scripted access — a Bearer token on the separate host
`dps150-api.example.com`. Backend middleware on `/api/*`: lets the request through
if there is a valid `Authorization: Bearer <token>` (with the required scope) OR
a trusted `Remote-User` header from Authelia; otherwise 401 `unauthorized`.
Mutations (PUT/POST/DELETE) require the `control` scope; GET — `read` or higher.

The gate is enabled by the `DPS_AUTH_REQUIRED` flag (env, default `false`). The local
single-user run, docker-compose, e2e, and mock work with an open
API (auth off); in the cluster the chart sets `DPS_AUTH_REQUIRED=true`.

## Auto-stop rules (F-018)

`AutomationRule`:
```json
{
  "id": 1, "name": "Battery charge to cutoff", "enabled": true,
  "condition": {"type": "currentBelow", "amps": 0.05, "forSeconds": 300},
  "action": "outputOff",
  "scope": "session",
  "createdAt": <ms>, "updatedAt": <ms>,
  "lastTriggeredAt": <ms|null>
}
```
- `condition.type`: `currentBelow {amps, forSeconds}` | `capacityAbove {ah}` |
  `energyAbove {wh}` | `elapsedAbove {seconds}`. Duration/hysteresis —
  on the engine side (a single spike does not trigger).
- `action`: for now only `outputOff` (reserved for extension).
- `scope`: `session` (applies only within the current output session, reset on turn-off) | `always`.
- Endpoints: `GET /api/v1/automation/rules` → `{"items":[...]}`;
  `POST`/`PUT /api/v1/automation/rules/{id}`/`DELETE ...` (CRUD, 404 `rule_not_found`);
  `GET /api/v1/automation/triggers?limit&offset` → history of triggers
  `{"items":[{"id","ruleId","ruleName","ts","reason"}],"total"}`.
- On trigger: turn off the output + journal event `autoStop
  {ruleId, ruleName, reason}` + Telegram. On loss of the device connection the
  rule goes to `suspended` (not evaluated, no triggers accumulate);
  the engine runs in the cluster — on a drop, auto-stop is NOT guaranteed
  (hardware protections as a backstop). The rule state is in the WS message
  `event` kind `autoStop` on trigger.
- Storage: table `automation_rules(id PK, name, enabled, condition JSON,
  action, scope, created_at, updated_at, last_triggered_at)`;
  `automation_triggers(id PK, rule_id INDEX, rule_name, ts INDEX, reason)`.

## CSV export (F-019)

- `GET /api/v1/history.csv?from&to&resolution` — streaming text/csv,
  `Content-Disposition: attachment; filename="dps150-history-<from>-<to>.csv"`.
  raw columns: `timestamp,voltage,current,power,temperature,output_on`
  (timestamp — ISO 8601 UTC). For 1m: `timestamp,v_min,v_avg,v_max,i_min,
  i_avg,i_max,p_min,p_avg,p_max,t_avg,samples`. Range constraints — same as
  `/history` (invalid_range), but without the 20000 cap (streaming). 503 when
  the DB is unavailable.
- `GET /api/v1/events.csv?from&to&kind` — streaming, columns
  `timestamp,kind,data` (data — a JSON string).
- Streaming: rows are written as they are read from the DB (cursor/pagination), without
  assembling everything in memory.

## API tokens (F-020)

`ApiToken` (metadata, no secret): `{"id":1, "name":"lab script",
"scope":"control", "createdAt":<ms>, "lastUsedAt":<ms|null>}`.
- `GET /api/v1/tokens` → `{"items":[ApiToken...]}` (metadata only).
- `POST /api/v1/tokens` body `{"name":"...", "scope":"read"|"control"}` →
  `201 {"token":"dps_<base64url>", ...ApiToken}` — the SECRET is shown
  once; only a SHA-256 hash is stored in the DB.
- `DELETE /api/v1/tokens/{id}` → `204`; a revoked token stops
  working immediately (hash checked against the DB, no cache beyond the request).
- Token management is available ONLY through the UI behind Authelia (not via a token).
- Tokens are NEVER logged (neither the secret nor the Authorization header).

## Frontend file structure (Stage 3)

New pages/components in their own files: `src/pages/AutomationPage.tsx`
(F-018, route /automation + menu item), export buttons on HistoryPage and
EventsPage (F-019), a tokens section in SettingsPage (F-020). i18n prefixes:
`automation.*`, `export.*`, `tokens.*`. TD-002: consolidation of history/events
types from components/chart/* into `src/api/`.
