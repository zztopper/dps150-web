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

---

# API contract v4: Battery charging mode (ADR-008 / F-023)

Fixed for parallel development of F-023 (backend `internal/charger` +
`internal/storage/charge.go` + `internal/api/charge.go`; frontend ChargePage).
The v1/v2/v3 rules apply (camelCase, unix millis, error format). All voltages
are volts, currents amperes, capacity milliamp-hours (`mAh`), energy watt-hours.

The charger is a backend-supervised run engine that owns the output for a whole
charge (mirroring the F-022 sequence Manager). It is **mutually exclusive** with
the sequence runner. New error codes:
`charge_active` (409) — a charge run owns the device;
`charge_preflight_failed` (409) — the safety pre-flight refused the start;
`invalid_charge_profile` (400); `charge_profile_not_found` (404);
`charge_session_not_found` (404). `storage_unavailable` (503) is reused
(charge profiles/sessions live in the DB; run/stop/active answer 503 when the
runner is not configured).

## Coordination (extends the F-022 409 gate)

The manual-mutation gate rejects `PUT /device/setpoints`, `PUT /device/output`,
`PUT /device/protections`, `PUT /device/presets/{slot}` and
`POST /profiles/{id}/apply` with **409 `charge_active`** while a charge run is
active — exactly as it does with `sequence_active` for a sequence run (a run of
either kind blocks manual control). Starting a charge while a sequence runs →
`409 sequence_active`; starting a sequence while a charge runs → `409
charge_active`. Reads, `POST /charge/stop` and `POST /sequences/stop` are never
blocked.

## Charge profiles (CRUD)

`ChargeProfile`:
```json
{
  "id": 1, "name": "18650 Li-ion 1S",
  "chemistry": "liion", "cells": 1,
  "capacityMah": 3400, "chargeCurrentA": 1.7,
  "bmsAttested": false,
  "params": null,
  "createdAt": <ms>, "updatedAt": <ms>
}
```
- `chemistry`: `"liion" | "lifepo4" | "pb"` (NiMH deferred out of v1 — see design §3.7).
- `bmsAttested` (bool, default false) — attests an external BMS/balancer is
  connected. Required for multi-cell lithium (validation below); ignored for 1S
  and Pb.
- `params` (nullable object) — optional per-cell overrides of the built-in
  preset (design §3.7). Any subset of: `vchargePerCell`, `taperC`,
  `prechargeThresholdPerCell`, `floatPerCell`, `vmaxPerCell`, `capacityCapPct`,
  `timeoutFactor`. Omitted fields take the preset default; overrides are
  re-validated against the device envelope and the chemistry's safe bounds, and
  must keep `Vcharge ≤ ceiling − margin ≤ OVP − margin`.
- Validation (`400 invalid_charge_profile`): non-empty name ≤ 64 chars;
  `cells ≥ 1`; `capacityMah > 0`; `chargeCurrentA > 0`; and the device envelope —
  `cells × Vcharge ≤ 30 V`, `chargeCurrentA ≤ 5 A`, `Vcharge × chargeCurrentA ≤
  150 W`; **multi-cell lithium requires attestation** — `chemistry ∈ {liion,
  lifepo4}` with `cells ≥ 2` must have `bmsAttested = true` (an imbalanced pack
  can overcharge one cell invisibly to the pack-level OVP).
- `GET /api/v1/charge/profiles` → `{"items": [ChargeProfile...]}` (by id, creation order).
- `POST /api/v1/charge/profiles` (no id) → `201 ChargeProfile`.
- `GET /api/v1/charge/profiles/{id}` → `200 ChargeProfile`; `404 charge_profile_not_found`.
- `PUT /api/v1/charge/profiles/{id}` → `200 ChargeProfile`; `404 charge_profile_not_found`.
- `DELETE /api/v1/charge/profiles/{id}` → `204`; `404 charge_profile_not_found`.
  Deleting a profile does not affect past `ChargeSession` rows (they copy
  `profileName`/`chemistry`/`cells`).

## Pre-flight (safety measurement, output off)

`POST /api/v1/charge/preflight` — body `{"profileId": 1}` OR an inline
`{"chemistry","cells","capacityMah","chargeCurrentA","params"?}`. Reads the
terminal voltage with the output **off** (the DPS-150 measures Vbat with no
output), validates it against the declared chemistry×cells and returns the
computed limits the UI must show before the confirmation step.

`200`:
```json
{
  "ok": true,
  "vbat": 3.72, "vbatPerCell": 3.72, "suggestedCells": 1,
  "chemistry": "liion", "cells": 1,
  "needsConfirm": false,
  "computed": {
    "icharge": 1.70,
    "vmaxCeiling": 4.25, "capacityCapMah": 3910, "timeoutMs": 10800000,
    "protections": {"ovp": 4.30, "ocp": 2.04, "opp": 8.7, "otp": 75.0}
  },
  "warnings": []
}
```
`needsConfirm` is `true` for a plausible-but-deeply-discharged pack (the caller
must resend `start` with `confirmDeepDischarge: true`). `computed` mirrors the
engine's enforced safety envelope (`charger.Limits`) plus the request's charge
current (`icharge`). It does **not** carry a `vcharge`: the engine's
`PreflightResult` exposes the ceiling / protections / caps it enforces, not the
internal per-phase CV setpoint. The UI derives the displayed target from
`vmaxCeiling` and the chemistry.
- `ok: false` with a `reason` when the reading is unsafe but was measured — the
  UI shows Vbat and the reason and disables Start: `vbat > vcharge`
  (reverse current / wrong chemistry / wrong cell count), `vbat ≈ 0`
  (no battery / short), `vbat < 0` (reversed polarity), a per-cell voltage
  outside the chemistry's plausible range, or **`cells ≠ suggestedCells`** (a
  cell-count mismatch is a hard refusal, never a soft warning — adjacent counts
  alias: a full 2S at 8.4 V reads as a "discharged" 3S at 2.80 V/cell). Vbat is
  read from a telemetry tick sampled *after* the output-off has settled
  (surface-charge decay), not the cached on-load voltage. Deeply-discharged
  Li-ion/LiFePO4 (below the precharge threshold, with a matching cell count) is
  `ok: true` with a warning and requires a second explicit confirmation — the run
  starts in the precharge phase.
- `409 device_offline` (cannot measure), `409 charge_active` / `409
  sequence_active` (device busy — a clean open-terminal reading is impossible),
  `400 invalid_charge_profile` (bad inline params). `503 storage_unavailable`
  only when `profileId` is given and storage is down.

## Run / stop / active

- `POST /api/v1/charge/profiles/{id}/start` — body `{"confirm": true}`
  (the confirmation interlock; a missing/false `confirm` → `400
  invalid_charge_profile`). The server re-runs the pre-flight guard, sets
  OVP/OCP/OPP/OTP from the profile, writes `Vset = Vcharge` **before** output-on,
  then energizes and runs. → `202 {"started": true}`.
  Errors: `409 charge_active`, `409 sequence_active`, `409 device_offline`,
  `409 charge_preflight_failed` (body `{"error":{"code":"charge_preflight_failed",
  "message":"<which guard: reverse current / no battery / ...>"}}`),
  `404 charge_profile_not_found`, `400 invalid_charge_profile`,
  `503 storage_unavailable`.
- `POST /api/v1/charge/stop` → `200 {"stopped": true}` — idempotent, `200` even
  when idle (output off follows in the run goroutine). `503` when the runner is
  not configured.
- `GET /api/v1/charge/active` → `200 ChargeStatus`, or `{"active": false}` when
  idle. `503` when the runner is not configured.

`ChargeStatus`:
```json
{
  "active": true,
  "sessionId": 12, "profileId": 1, "profileName": "18650 Li-ion 1S",
  "chemistry": "liion", "cells": 1, "startedAt": <ms>,
  "state": "running",
  "phase": "cc", "phaseIndex": 1, "totalPhases": 2, "mode": "cc",
  "deliveredMah": 850.0, "deliveredWh": 3.1,
  "peakVoltage": 4.05, "targetMah": 3400, "capacityCapMah": 3910, "ceilingVolts": 4.25,
  "elapsedMs": 1830000, "etaMs": 2400000,
  "measured": {"voltage": 4.05, "current": 1.7, "power": 6.9}
}
```
- `state`: `"running" | "completed" | "stopped" | "aborted" | "failed"`
  (`completed` = normal termination: taper / user-stop-in-float;
  `aborted` = a safety fault: timeout / voltage ceiling / capacity cap / OTP /
  reverse-guard / protection trip; `stopped` = user stop; `failed` = internal /
  device error mid-run).
- `phase`: `"precharge" | "cc" | "float"` (the engine's compiled phase kinds;
  the hardware runs CC→CV under one setpoint, so `cc` covers both, and `mode`
  reflects the live regulation mode `"cc" | "cv"`). `phaseIndex`/`totalPhases`
  give "phase X of N".
- `etaMs`: `-1` when unknown (in CV/float, or Pb float held until stop).
- The **WS `chargeProgress`** event carries this exact same field set (minus
  `active`) at ~1 Hz, so the frontend uses one shape for the live view whether it
  polls `GET /charge/active` or consumes the push.

## Charge session history

- `GET /api/v1/charge/sessions?limit=50&offset=0` →
  `{"items": [ChargeSession...], "total": 123}` (newest first).
- `GET /api/v1/charge/sessions/{id}` → `200 ChargeSession`;
  `404 charge_session_not_found`.

`ChargeSession`:
```json
{
  "id": 12, "profileId": 1, "profileName": "18650 Li-ion 1S",
  "chemistry": "liion", "cells": 1,
  "startedAt": <ms>, "endedAt": <ms>,
  "state": "completed",
  "reason": "current tapered below 0.05C in CV",
  "deliveredMah": 3350.0, "deliveredWh": 12.4, "peakVoltage": 4.20,
  "snapshot": {"preflight": {"vbat": 3.72}, "computed": {...}}
}
```
- A row is created at start with `state:"running"`, `endedAt:null`, and finalized
  at the terminal state — a leftover `running` row is the visible trace of a
  backend crash. `state` matches `ChargeStatus.state`. Session persistence is
  fail-soft (a down DB drops it with a warning and never affects the charge),
  matching the sequence-run journal.

## WS additions

Two journal kinds ride the existing v1 `event` message (`data` = the journal
fields + `kind` + `ts`); there are **no new message types**.
- `chargeProgress` — throttled to ~1 Hz and emitted on every phase/state change;
  the `ChargeStatus` field set (minus `active`) plus `kind`/`ts`:
  `{kind:"chargeProgress", sessionId, profileId, profileName, chemistry, cells,
  state, phase, phaseIndex, totalPhases, mode, deliveredMah, deliveredWh,
  peakVoltage, targetMah, capacityCapMah, ceilingVolts,
  measured:{voltage,current,power}, elapsedMs, etaMs, ts}`.
- `chargeSession` — the terminal outcome, also appended to the `events` journal
  (below): `{kind:"chargeSession", sessionId, profileName, chemistry, cells,
  state, reason, deliveredMah, deliveredWh, durationMs, ts}`.

The `events` journal (F-014) gains one kind:
`chargeSession {sessionId, profileName, chemistry, cells, state, reason,
deliveredMah, deliveredWh, durationMs}` (summary at each terminal state; feeds
the Events page, CSV export and — best-effort, like autoStop — Telegram).

## DB schema (all times unix millis, portable SQL)

- `charge_profiles(id PK autoincr, name, chemistry, cells, capacity_mah,
  charge_current_a, params TEXT/JSON, created_at, updated_at)`
- `charge_sessions(id PK autoincr, profile_id INDEX, profile_name, chemistry,
  cells, started_at INDEX, ended_at, state, reason, delivered_mah, delivered_wh,
  peak_voltage, snapshot TEXT/JSON)`

Both are feature-owned models wired via `storage.Config.Models` (a new anchor in
`cmd/server/main.go`), auto-migrated on both sqlite and postgres — no dialect
functions, no separate SQL migration.

## Frontend file structure

New page/components in their own files: `src/pages/ChargePage.tsx` (route
`/charge` + menu item) with `src/components/ChargeProfiles.tsx` (CRUD),
`src/components/ChargeLive.tsx` (pre-flight + confirmation + live V/I chart with
phase bands, phase/elapsed/ETA, mAh delivered, safety-cap progress bars) and
`src/components/ChargeSessions.tsx` (history). API types + TanStack Query hooks in
`src/api/charge.ts`; MSW mocks for every route. i18n prefix `charge.*`. Visual
design is handled separately (ui-ux skill).

---

# API contract v5: IV curve tracer (ADR-009 / F-024)

Fixed for parallel development of F-024 (backend `internal/ivtrace` +
`internal/storage/iv.go` + `internal/api/iv.go`; frontend IVPage). The
v1/v2/v3 rules apply (camelCase, unix millis, error format). All voltages are
volts, currents amperes, power watts, resistances ohms.

The IV curve tracer is a backend-supervised run engine that owns the output for
a whole sweep (mirroring the F-023 charge Manager). It is a **telemetry-driven
step loop**: for each linear step it writes a setpoint, waits for a fresh settled
telemetry tick, and records the measured `(V,I)` operating point — a real point
on the DUT's I–V curve. It is **mutually exclusive** with the charge runner and
the sequence runner through the shared `device.Interlock` (owner tag `iv`). It is
**low-risk** (no battery): there is **no pre-flight**, the output energizes at the
sweep start with the compliance already written. New error codes:
`iv_active` (409) — a sweep owns the device;
`invalid_iv_profile` (400); `iv_profile_not_found` (404);
`iv_sweep_not_found` (404). `device_offline` (409) and `storage_unavailable`
(503) are reused (profiles/sweeps live in the DB; run/stop/active answer 503 when
the runner is not configured).

## Coordination (extends the F-023 interlock gate)

The manual-mutation gate (`blockDuringInterlock`, keyed on the shared interlock)
already 409s with `owner+"_active"`, so it rejects `PUT /device/setpoints`,
`PUT /device/output`, `PUT /device/protections`, `PUT /device/presets/{slot}` and
`POST /profiles/{id}/apply` with **409 `iv_active`** while a sweep runs — with no
change to the gate. Starting a sweep while a charge/sequence runs →
`409 charge_active`/`409 sequence_active`; starting a charge/sequence while a
sweep runs → `409 iv_active`. Reads, `POST /iv/stop`, `POST /charge/stop` and
`POST /sequences/stop` are never blocked.

## IV profiles (CRUD)

`IVProfile`:
```json
{
  "id": 1, "name": "Red LED 5mm",
  "component": "led", "mode": "voltage",
  "vStart": 0.0, "vStop": 6.0, "iStart": 0.0, "iStop": 0.0,
  "steps": 50, "dwellMs": 1000,
  "complianceA": 0.02, "complianceV": 0.0,
  "params": null,
  "createdAt": <ms>, "updatedAt": <ms>
}
```
- `component`: `"led" | "diode" | "zener" | "resistor" | "lamp" | "generic"`.
- `mode`: `"voltage" | "current"`. A **voltage** sweep uses `vStart`/`vStop` and
  `complianceA` (the current limit that protects the DUT); a **current** sweep uses
  `iStart`/`iStop` and `complianceV` (the voltage ceiling). The unused pair is
  ignored (stored as 0).
- `steps` (int, 2–1000, default 50) — linear steps `start → stop` inclusive,
  uni-directional in v1. `dwellMs` (int ≥ 200, default 1000) — the per-step settle
  wait; the sample is the first telemetry tick with `TS ≥ writeTS + dwellMs`.
- `params` (nullable object) — optional per-component analysis overrides (design
  §3.8). Any subset of: `refCurrentA` (Vf/rd reference current), `junctionTempK`
  (thermal voltage), `iztA` (Zener test current), `powerRatingW` (Zener/resistor
  power derating), `fitLowFrac`/`fitHighFrac` (the exponential/ohmic fit windows).
  Stored opaquely; the analysis layer owns the shape.
- Validation (`400 invalid_iv_profile`): non-empty name ≤ 64 chars; valid
  `component` and `mode`; `steps` in `[2,1000]`; `dwellMs ≥ 200`; and the device
  envelope — for a voltage sweep `0 ≤ vStart < vStop ≤ 30`, `0 < complianceA ≤ 5`,
  `vStop × complianceA ≤ 150`; for a current sweep `0 ≤ iStart < iStop ≤ 5`,
  `0 < complianceV ≤ 30`, `complianceV × iStop ≤ 150`.
- `GET /api/v1/iv/profiles` → `{"items": [IVProfile...]}` (by id, creation order).
- `POST /api/v1/iv/profiles` (no id) → `201 IVProfile`.
- `GET /api/v1/iv/profiles/{id}` → `200 IVProfile`; `404 iv_profile_not_found`.
- `PUT /api/v1/iv/profiles/{id}` → `200 IVProfile`; `404 iv_profile_not_found`.
- `DELETE /api/v1/iv/profiles/{id}` → `204`; `404 iv_profile_not_found`.
  Deleting a profile does not affect past `IVSweep` rows (they copy
  `profileName`/`component`/`mode`).

## Run / stop / active

- `POST /api/v1/iv/profiles/{id}/start` — body `{"confirm": true}` (the
  output-energize confirmation interlock, §3.5; a missing/false `confirm` →
  `400 invalid_iv_profile`). The server sets OVP/OCP/OPP/OTP a step above the
  bounds, writes the compliance **before** the first swept setpoint and
  output-on, then energizes and runs the step loop. → `202 {"started": true}`.
  Errors: `409 iv_active`, `409 charge_active`, `409 sequence_active`,
  `409 device_offline`, `404 iv_profile_not_found`, `400 invalid_iv_profile`,
  `503 storage_unavailable`.
- `POST /api/v1/iv/stop` → `200 {"stopped": true}` — idempotent, `200` even when
  idle (output off follows in the run goroutine). `503` when the runner is not
  configured.
- `GET /api/v1/iv/active` → `200 IVStatus`, or `{"active": false}` when idle.
  `503` when the runner is not configured.

`IVStatus`:
```json
{
  "active": true,
  "sweepId": 7, "profileId": 1, "profileName": "Red LED 5mm",
  "component": "led", "mode": "voltage", "startedAt": <ms>,
  "state": "running",
  "stepIndex": 23, "totalSteps": 50, "pointCount": 23,
  "lastPoint": {"v": 1.94, "i": 0.011},
  "complianceA": 0.02, "complianceV": 0.0,
  "measured": {"voltage": 1.94, "current": 0.011, "power": 0.021},
  "elapsedMs": 23000, "etaMs": 27000
}
```
- `state`: `"running" | "completed" | "stopped" | "aborted" | "failed"`
  (`completed` = the sweep ran to its last step; `stopped` = user stop;
  `aborted` = a safety fault: telemetry-stale / per-sweep timeout / protection
  trip / device offline / output-off-failed; `failed` = internal error mid-run).
- `stepIndex`/`totalSteps` drive the progress indicator; `pointCount` is the
  number of recorded points; `lastPoint` is the most recent measured `(v,i)`.
- `etaMs`: remaining steps × dwell; `-1` when unknown.
- **Live curve**: `IVStatus`/`ivProgress` carry only `lastPoint` (+ `pointCount`,
  `stepIndex`), so the client appends points incrementally. The **authoritative**
  full point set and metrics come from `GET /iv/sweeps/{id}`, which the client
  fetches on the terminal `ivSweep` event (and on WS reconnect), reconciling any
  dropped `ivProgress` frames.
- The **WS `ivProgress`** event carries this exact field set (minus `active`) at
  ~1 Hz and on every step/state change, so the frontend uses one shape whether it
  polls `GET /iv/active` or consumes the push.

## Sweep history

- `GET /api/v1/iv/sweeps?limit=50&offset=0` →
  `{"items": [IVSweep...], "total": 123}` (newest first).
- `GET /api/v1/iv/sweeps/{id}` → `200 IVSweep`; `404 iv_sweep_not_found`.

`IVSweep`:
```json
{
  "id": 7, "profileId": 1, "profileName": "Red LED 5mm",
  "component": "led", "mode": "voltage",
  "startedAt": <ms>, "endedAt": <ms>,
  "state": "completed", "reason": "complete",
  "points": [{"v": 0.0, "i": 0.0}, {"v": 1.82, "i": 0.004}, {"v": 1.98, "i": 0.02}],
  "metrics": {
    "vfAtRef": 1.98, "refCurrentA": 0.02,
    "ideality": 1.9, "satCurrentA": 3.1e-12,
    "seriesR": 8.4, "seriesRApparent": true, "dynamicR": 12.1,
    "quality": {"vfAtRef": "ok", "ideality": "approx"},
    "notes": ["ideality: approximate — linear-V sampling, 9 in-region points"]
  },
  "snapshot": {
    "vStart": 0, "vStop": 6, "steps": 50, "dwellMs": 1000,
    "complianceA": 0.02,
    "protections": {"ovp": 6.6, "ocp": 0.03, "opp": 0.2, "otp": 60.0}
  }
}
```
- `endedAt` is `null` while a run is in flight. `points` is a JSON array of the
  measured `{v,i}` samples (empty `[]` until the first step completes).
- `metrics` (null until finalized) is component-specific — only the relevant keys
  are present. **Every numeric metric is `number | null`**: it is `null` (or
  omitted) when the guards (design §3.8) could not compute it reliably —
  non-conducting DUT, too few in-region points, degenerate/ill-conditioned fit,
  breakdown not reached. The backend **never emits a fabricated value**; the
  frontend renders a null metric as "—" / "не определено", not `0`. Two companion
  fields explain confidence:
  - `quality` — an object mapping metric name → `"ok" | "approx" | "unreliable"`
    (absent key ⇒ `ok`). `ideality` is `approx` by construction (linear-V sampling
    + 12-bit quantisation, §3.8).
  - `notes` — `string[]` human-readable reasons for any null/`approx`/`unreliable`
    metric (e.g. `"did-not-conduct"`, `"breakdown-not-reached"`,
    `"too few in-region points (3)"`).
  Per component (any of these may be `null` per the above):
  - **led/diode**: `vfAtRef`, `refCurrentA`, `ideality`, `satCurrentA`,
    `seriesR`, `seriesRApparent` (bool), `dynamicR`.
  - **resistor**: `resistance`, `rSquared`, `maxDevPct`.
  - **zener**: `vz`, `iztA`, `zzt`.
  - **lamp**: `rCold`, `rHot`, `rHotColdRatio`.
- A row is created at start with `state:"running"`, `endedAt:null`, and finalized
  at the terminal state — a leftover `running` row is the visible trace of a
  backend crash. Sweep persistence is fail-soft (a down DB drops it with a warning
  and never affects the sweep), matching the charge-run journal.

## CSV export

- `GET /api/v1/iv/sweeps/{id}.csv` — streaming `text/csv`,
  `Content-Disposition: attachment; filename="dps150-iv-sweep-<id>.csv"`. Columns:
  `index,voltage,current,power` (`power = voltage × current`), one row per
  recorded point, in sweep order. `404 iv_sweep_not_found`;
  `503 storage_unavailable`.

## WS additions

Two journal kinds ride the existing v1 `event` message (`data` = the journal
fields + `kind` + `ts`); there are **no new message types**.
- `ivProgress` — throttled to ~1 Hz and emitted on every step/state change; the
  `IVStatus` field set (minus `active`) plus `kind`/`ts`:
  `{kind:"ivProgress", sweepId, profileId, profileName, component, mode, state,
  stepIndex, totalSteps, pointCount, lastPoint:{v,i}, complianceA, complianceV,
  measured:{voltage,current,power}, elapsedMs, etaMs, ts}`.
- `ivSweep` — the terminal outcome with the computed metrics, also appended to the
  `events` journal (below): `{kind:"ivSweep", sweepId, profileName, component,
  mode, state, reason, pointCount, metrics:{...}, durationMs, ts}`.

The `events` journal (F-014) gains one kind:
`ivSweep {sweepId, profileName, component, mode, state, reason, pointCount,
durationMs}` (summary at each terminal state; feeds the Events page, CSV export
and — best-effort, like autoStop — Telegram).

## DB schema (all times unix millis, portable SQL)

- `iv_profiles(id PK autoincr, name, component, mode, v_start, v_stop, i_start,
  i_stop, steps, dwell_ms, compliance_a, compliance_v, params TEXT/JSON,
  created_at, updated_at)`
- `iv_sweeps(id PK autoincr, profile_id INDEX, profile_name, component, mode,
  started_at INDEX, ended_at, state, reason, points TEXT/JSON, metrics TEXT/JSON,
  snapshot TEXT/JSON, created_at)`

Both are feature-owned models wired via `storage.Config.Models` (a new anchor in
`cmd/server/main.go`), auto-migrated on both sqlite and postgres — no dialect
functions, no separate SQL migration.

## Emulator (testing)

`DPS_MOCK_DUT` attaches a passive device-under-test to the `mock://` emulator so a
sweep runs end-to-end without hardware (sibling to `DPS_MOCK_BATTERY`):
`"resistor,<ohms>"` or `"diode,<Is>,<n>,<Rs>[,<Vt>]"` (e.g.
`"diode,1e-9,1.8,1.0"` for an LED, `"resistor,100"` for a 100 Ω resistor). A DUT
and a battery are mutually exclusive; an invalid value is logged and ignored
(mock-only dev knob), never fatal.

## Frontend file structure

New page/components in their own files: `src/pages/IVPage.tsx` (route `/iv` + menu
item) with `src/components/IVProfiles.tsx` (CRUD), `src/components/IVLive.tsx`
(confirmation + live I(V) chart — uPlot, V on the x-axis and I on the y-axis, with
the compliance band and annotated metrics — plus the step-progress indicator) and
`src/components/IVSweeps.tsx` (history + CSV export). API types + TanStack Query
hooks in `src/api/iv.ts`; MSW mocks for every route. i18n prefix `iv.*`. Visual
design is handled separately (ui-ux skill).
