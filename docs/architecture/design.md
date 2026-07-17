# dps150-web — system design

Status: baseline design, revised via ADRs.
All decisions below are the fixed baseline; changes go through ADRs.

## 1. What this is

A web application for full control of the **FNIRSI DPS-150** laboratory power
supply (USB-CDC serial), deployed in a home Talos k8s cluster:

1. Full PSU control: V/I setpoints, output enable, protection setpoints
   (OVP/OCP/OPP/OTP/LVP), brightness/sound, hardware presets M1–M6.
2. Profiles (name + V + I + protection setpoints) stored in the DB and assigned
   to hardware slots M1–M6.
3. Visualization: live readings (V/I/P, input voltage, temperature,
   CC/CV, protection state) + historical charts over a month.

The project is written with open source in mind (MIT, README in English,
no home addresses/secrets in the code — only in values/CI variables).

## 2. Device and protocol

- FNIRSI DPS-150: 0–30 V / 0–5 A / 150 W, USB-C (CDC serial, 115200 8N1).
- Binary "memory-mapped registers" protocol, little-endian, float32 IEEE-754.
  Full reverse-engineered reference: https://github.com/cho45/fnirsi-dps-150
  (docs/FNIRSI_DPS-150_Protocol.md); original CLI implementation (subset):
  https://github.com/svenk123/dps150tool.
- Frame: `F1|F0 <GROUP> <REG> <LEN> <DATA…> <CHK>`, CHK = (REG+LEN+ΣDATA)&0xFF.
- Key registers: C1/C2 V/I setpoints; DB output (0/1); C5–D0 presets M1–M6;
  D1–D5 protection setpoints; D6/D7 brightness/sound; C0/C3/C4/E2/E3 telemetry
  (the device pushes it itself every 500 ms); D8/D9/DA Ah/Wh accounting;
  DC protection trip (OK/OVP/OCP/OPP/OTP/LVP/REP); DD CC/CV mode;
  FF full state dump (139 bytes).
- Session: enable (`F1 C1 00 01 01 02`) → baud → operation → disable.

## 3. Architecture

```
                       ┌─ Home network ──────────────────────────────┐
 USB-C                 │                                              │
 DPS-150 ──── pve (Proxmox, 10.20.0.5)                                │
              └─ ser2net (raw TCP :2150, cluster subnet only)         │
                        │                                             │
              ┌─ k8s (Talos) ── ns dps150 ────────────────┐           │
              │  backend (Go) ── tcp://10.20.0.5:2150     │           │
              │     │  ├ REST API + WebSocket             │           │
              │     │  └ Telegram notifications           │           │
              │  frontend (React SPA, nginx)              │           │
              │  Ingress: dps150.example.com (Traefik,    │           │
              │    cert-manager letsencrypt-cloudflare,   │           │
              │    Authelia forward-auth over whole host) │           │
              └────────│───────────────────────────────────┘          │
                       │                                              │
              PostgreSQL: CNPG pg-cluster                             │
              (pg-cluster-pooler-rw.pg-cluster.svc:5432)              │
```

### 3.1 Device transport (key abstraction)

The DPS-150 driver in the backend runs on top of a transport interface
(`io.ReadWriteCloser` + reconnect semantics). Three implementations,
selected by config (`DPS_TRANSPORT`):

| Transport | URI | Usage |
|---|---|---|
| serial | `serial:///dev/ttyUSB0` (or by-id) | local run on linux/macos, PSU plugged in directly |
| tcp | `tcp://10.20.0.5:2150` | prod in the cluster, via ser2net on pve |
| mock | `mock://` | device emulator: e2e in CI, development without hardware |

Owner's requirement (recorded separately): the server side must work
identically both with a tcp socket via ser2net and with a serial
port directly.

The emulator implements the protocol at the frame level: 2 Hz telemetry, reaction to
register writes, simulated protection trips — the same binary,
enabled by config.

### 3.2 Backend (Go)

- Sole owner of the device connection (the port is single-client);
  all consumers (REST, WS, history, rules) work through an internal hub.
- REST API: setpoints, output, protections, profiles, history, device/status.
- WebSocket: live telemetry 2 Hz + events (protections, CC/CV, connect/disconnect).
- History writer: batches into the DB; retention and per-minute aggregates — background jobs.
- Telegram notifications: protection trip, loss/recovery of the device
  connection, (Stage 3) auto-stop events. Type configuration — in the UI.
- Prometheus metrics `/metrics` (+ ServiceMonitor) — picked up by
  kube-prometheus-stack, viewed in SigNoz.
- On connection loss: exponential reconnect, status in UI/WS, event in the journal.

### 3.3 Frontend (React + TS + Vite + Ant Design + TanStack Query)

- Main screen: large V/I/P readings (+ CC/CV, input, temperature),
  output toggle (with confirmation), quick setpoints, profile application.
- Charts: uPlot (live window + history from the DB with zoom/pan).
- Pages: Dashboard, History, Profiles, Events (journal), Settings.
- Responsive: desktop — full UI; phone — bench mode of the main screen
  (large numbers, output switch, profile) from the first stage.
- i18n: react-i18next, ru+en locales from the first screen.
- Realtime — WebSocket, data/mutations — TanStack Query over REST.

### 3.4 Storage

- **Prod**: PostgreSQL 17 in the shared CNPG `pg-cluster` following the partdb pattern:
  managed role `dps150` + `Database` CR + `VaultStaticSecret`
  (Vault: `secret/pg-cluster/dps150/database`); connection via
  `pg-cluster-pooler-rw.pg-cluster.svc:5432`, migrations — via
  `pg-cluster-direct` (bypassing the pooler).
- **Local**: SQLite (pure-Go, no cgo: GORM driver `glebarez/sqlite`
  over `modernc.org/sqlite`) — a single binary with no dependencies.
- Storage configuration (contract): `DPS_DB_DRIVER` = `sqlite` (default) |
  `postgres`; `DPS_DB_DSN` = path to the file (default `dps150.db`) | postgres DSN.
  An unavailable DB does not crash the app: background reconnect, storage features
  respond 503, device control keeps working.
- Portable schema: time — unix millis (integer), no dialect-specific functions;
  time aggregation — by integer division of the timestamp.
- Data: `samples` (2 Hz, 30-day retention), `samples_1m` (per-minute aggregates
  min/avg/max, 1-year retention), `profiles`, `events` (protections, connect/disconnect,
  output toggles — who/when/what), `settings`, (Stage 3) `automation_rules`,
  (ADR-008) `charge_profiles` + `charge_sessions`.
- Volume: ~5.2 M rows/month — time-based partitions/indexes, no TSDB.

### 3.5 Security

- Ingress `dps150.example.com` entirely behind Authelia forward-auth
  (middleware `authelia-forwardauth-authelia@kubernetescrd`), cluster SSO.
- ser2net on pve: listens only for the cluster subnet (firewall/bind).
- Secrets (PG, Telegram token) — Vault → VaultStaticSecret → k8s Secret.
- Applying a profile NEVER enables the output automatically.
  Enabling the output is only an explicit action with confirmation in the UI.
- (Stage 3) API tokens for scripted access.

### 3.6 Home Assistant / MQTT + Prometheus telemetry (ADR-007)

Decision: expose the supply to Home Assistant over MQTT Discovery, and export
the live telemetry as Prometheus gauges for a Grafana dashboard.

- **MQTT** — a new independent hub subscriber (`internal/mqtt`, alongside the
  journal/automation/metrics subscribers), enabled only when `DPS_MQTT_BROKER`
  is set (silent-off otherwise, mirroring the Telegram credential gate). It
  publishes a retained JSON state topic (`<prefix>/state`), an availability
  topic with an MQTT Last-Will (`<prefix>/status`), and retained HA Discovery
  configs so the entities (voltage/current/power/temperature/input-voltage/
  Ah/Wh sensors, CC-CV and protection sensors, a device-link connectivity
  binary sensor) appear automatically.
- **Control** is opt-in via `DPS_MQTT_CONTROL` (default off, mirroring
  `DPS_AUTH_REQUIRED`). When on, an output `switch` and voltage/current
  `number` entities are published and their command topics call the hub's
  `SetOutput`/`SetVoltage`/`SetCurrent`. **Trust model:** MQTT commands do
  **not** pass through Authelia or the token gate — the broker's own
  authentication/ACLs are the trust boundary, so control must only be enabled
  against a broker the owner controls. As at the hub level, energizing the
  output over MQTT has no confirmation interlock: `SetOutput(true)` applies
  immediately. Applying power over MQTT is therefore a deliberate, ACL-gated
  capability, kept off by default.
- **Prometheus** — the existing metrics hub-watcher additionally sets
  `dps150_{voltage_volts,current_amps,power_watts,temperature_celsius,
  input_voltage_volts,capacity_amp_hours,energy_watt_hours,output_enabled,
  setpoint_voltage_volts,setpoint_current_amps}` from the same telemetry
  stream. `deploy/grafana/dashboard.json` renders them plus the existing
  link/protection/latency series.

### 3.7 Battery charging mode (ADR-008)

Decision: add a first-class, backend-supervised battery charger as a **new
independent hub subscriber** (`internal/charger`, alongside the sequence
Manager and automation Engine), not built on the sequence interpreter and not
a sequence node type. Charging needs several **simultaneous** safety cutoffs
(taper *and* voltage ceiling *and* capacity cap *and* timeout *and* over-temp)
that the sequence's single `advance` predicate cannot express, plus a first-class
pre-flight/reporting UX. v1 ships **Li-ion, LiFePO4 and Pb**; **NiMH is deferred**
(see the note after the preset table).

- **Engine (`internal/charger`)** — a telemetry-driven run engine mirroring the
  sequence Manager: one active run-slot (`Start`/`Stop`/`Run(ctx)`/`IsRunning`/
  `ActiveStatus`), it `Subscribe`s to the hub (~2 Hz), owns the output for the
  whole run, and broadcasts progress as `device.JournalEvent` (kind
  `chargeProgress`), the terminal outcome as `chargeSession`. It is
  chemistry-agnostic: a charge is an ordered list of **phases**
  `{targetV, currentLimit, termination}`; a chemistry is *data* (a preset that
  compiles to phases). Only wired when storage is configured (profiles live in
  the DB), same as the sequence runner.
- **Phases** (hardware does CC/CV regulation; the engine observes the measured
  current/voltage, with `telemetry.Mode` advisory only):
  - **Li-ion / LiFePO4** — optional precharge/trickle at 0.1C while the cell is
    below the precharge threshold → main `{Vcharge, Icharge}`; terminate when the
    measured current falls below the taper threshold **and** the measured voltage
    is ≥ `Vcharge − ε`, held for a debounce window.
  - **Pb** — `{Vabsorb, Icharge}`, terminate on taper (or timed absorption) →
    optional float `{Vfloat}` held until the user stops.
- **Termination reads measured values, not just `Mode`.** `telemetry.Mode`/
  `Protection` are pushed on change only; an observer issues no writes, so it
  never forces a `GetAll` and a dropped `DD`/`DC` frame would leave the cached
  value stale (a taper gated on `Mode==CV` alone could never fire). The hub gains
  a **`Refresh(ctx)`** method (a bare `GetAll`, no mutating write) so the charger
  can re-poll `Mode`/`Protection`/counters during long observe-only phases.
- **Strict safety envelope (non-disable-able on every charge)**: (1) **per-phase**
  hard timeouts — precharge (0.1C into a deeply-discharged cell is slow) and the
  CV taper tail budgeted separately, not one whole-run factor; Pb's indefinite
  float phase does not disable the *other* limits → abort; (2) an absolute
  per-chemistry×cells voltage ceiling, written into the hardware **OVP** *and*
  checked in software → abort; (3) a per-chemistry capacity cap (115–125 % of
  rated mAh) → abort; (4) the DPS-150 over-temperature protection (OTP) → abort;
  (5) the start/pre-flight guard; (6) OVP/OCP/OPP/OTP always set from the profile
  before output-on. Overrides are re-validated so `Vcharge ≤ ceiling − margin ≤
  OVP − margin` always holds (an override can never invert the safety margins).
  The engine distinguishes **normal termination** (taper / user-stop-in-float →
  `completed`) from a **fault abort** (timeout / ceiling / cap / temp / trip →
  `aborted`). **Delivered-charge accounting is reset-aware**: `deliveredMah` is a
  delta from a session baseline of the device's free-running Ah counter, which
  zeroes when metering is re-enabled (the hub sends `RegMeteringEnable=1` on every
  reconnect) after a device power-cycle — so the charger tracks the last-seen
  counter and treats **any decrease as a fault/abort**, never silently
  re-baselining.
- **Critical hardware facts.** (a) The DPS-150 temperature sensor measures the
  *supply*, not the battery — there is **no battery-temperature cutoff** (a
  dT/dt/battery-temperature-rise termination is not available on this rig; it is
  the reason NiMH is deferred). (b) The DPS-150 reads terminal voltage with the
  output **off**, so every charge begins with a **pre-flight**: it measures Vbat
  (from a telemetry tick timestamped *after* the output-off has settled — surface
  charge decays — not the cached on-load voltage), validates it against the
  declared chemistry×cells, suggests a cell count, and **hard-refuses** the start
  when `Vbat > Vcharge` (reverse current / wrong chemistry / wrong cell count),
  `Vbat ≈ 0` (no battery / short), `Vbat < 0` (reversed polarity), **or
  `declaredCells ≠ suggestedCells`** — adjacent cell counts alias (a full 2S at
  8.4 V reads as a "discharged" 3S at 2.80 V/cell → would drive to 6.3 V/cell), so
  a cell-count mismatch is a hard refusal, never a soft warning; the genuinely
  deeply-discharged case needs a second explicit confirmation. Start requires the
  confirmation step showing the computed limits. (c) **Start order is an
  invariant**: (1) confirm output off → (2) `SetProtections` → (3)
  `SetVoltage(Vcharge)` (≥ any valid battery, so reverse current is impossible at
  energize) → (4) `SetCurrent(Icharge)` → (5) **only then** `SetOutput(true)`,
  each step error-checked. The charger must **not** inherit the sequence Manager's
  energize-first structure (which sets V/I later, inside the step, and would
  energize with a stale `Vset`); a test asserts the on-the-wire frame order via
  the emulator's tx-parser.
- **Multi-cell lithium requires an attested external BMS.** The DPS-150 charges
  the *pack* with no per-cell sensing or balancing, so an imbalanced pack can
  drive one cell to 4.3 V while the pack average looks fine and the pack-level OVP
  cannot see it. A profile with `chemistry ∈ {liion, lifepo4}` and `cells ≥ 2`
  requires `bmsAttested = true` or it is invalid (`400 invalid_charge_profile`)
  and cannot start; 1S needs no attestation. The UI shows a loud warning on the
  attestation checkbox.
- **Watchdog — telemetry-staleness, backend-supervised.** The whole safety net
  runs in the backend engine, so closing the browser is safe. The **primary**
  trigger is telemetry staleness: **no `device.Telemetry` tick for > 3–5 s**
  (6–10 missed ticks) → fault → `SafeOutputOff`. Link-loss (`StatusChange`) is only
  a *secondary* trigger, because the deploy transport is ser2net raw TCP: if the
  DPS-150 hangs or power-cycles the TCP socket stays up while `hub.session()` (no
  read-idle timeout after handshake) never emits `Connected:false` — so a
  link-loss-only watchdog would never fire and the output would stay energized.
  (Separately, the hub *should* gain a session read-idle timeout, but the charger
  must not depend on it.) A backend hard-crash is an acknowledged residual risk
  (the DPS-150 has no comms-watchdog); a graceful shutdown cuts the output, the
  hardware OVP/OCP set from the profile are the last line of defence, and startup
  reconciliation (below) bounds the exposure to one pod restart.
- **Coordination.** The charger and the sequence runner both own the output and
  are **mutually exclusive** via a **single shared device-ownership interlock**
  (one lock/owner token acquired atomically at start) — not two independent
  `IsRunning()` checks, which race (both could read the other idle and both
  energize). The 409 gate is extended with a new error code `charge_active`
  mirroring `sequence_active`; the shared gate rejects manual
  device/profile/preset/protection mutations while *either* run is active, and
  starting one while the other is active is rejected symmetrically
  (`charge_active` / `sequence_active`). The automation-engine suppressor becomes
  `seq.IsRunning() || charger.IsRunning()`.
- **`device.SafeOutputOff` — the shared teardown helper.** Output-off on every
  terminal path and on a protection trip goes through one helper reused by both
  the sequence Manager and the charger (the Manager's `run.outputOff` is
  refactored onto it). It **always creates its own fresh bounded
  `context.Background()` context** and ignores the (possibly cancelled) caller
  context — otherwise `hub.SetOutput` returns `ctx.Err()` and no-ops, leaving the
  battery energized. On failure it **retries, raises a fault and fires an
  alarm/Telegram** — never log-and-continue. There is **no pause/resume in v1** —
  stop then restart re-runs the pre-flight.
- **Storage, reconciliation & UI.** Two feature-owned models registered through
  `Config.Models`: `charge_profiles` (saved CRUD profiles) and `charge_sessions`
  (session history, a row per run, created at start `running` and finalized at the
  terminal state). **Startup reconciliation**: on boot, if the output is on and a
  `charge_sessions` row is `running` with no active runner, the charger forces
  `SafeOutputOff` and finalizes the row as `failed` (pattern:
  `notify/metering.go:resumeSessionOnConnect`) — turning "energized indefinitely
  after a crash" into "energized only until the pod restarts". The charger's
  `chargeSession` notification **suppresses** the overlapping
  `notify/metering.go` `meteringSession` Telegram while a charge owns the output
  (no double-notify). A dedicated **Charge** page (like Sequences): saved profiles,
  the live process (V+I chart with phase bands, phase/elapsed/ETA, mAh delivered,
  safety-cap progress bars) and session history.
- **Chemistry presets (per cell — safety-critical, cite sources).** Values below
  are validated against Battery University; every one is a first-class safety
  parameter compiled into a phase or a cutoff.

  | Chemistry | CC default | Precharge → threshold | CV target | Taper (terminate) | Float | Abs. ceiling (SW abort / HW OVP) | Capacity cap |
  |---|---|---|---|---|---|---|---|
  | Li-ion | 0.5–1C | Vcell<3.0 V → 0.1C | 4.20 (±0.05) | I<0.05C (3–5 %) in CV | — | 4.25 / 4.30 | 115 % |
  | LiFePO4 | 0.2–0.5C (≤1C) | Vcell<2.5 V → 0.1C | 3.65 | I<0.05C in CV | none (opt. ≤3.40) | 3.70 / 3.80 | 115 % |
  | Pb | 0.1–0.3C | — | 2.40 (absorb) | I<~C/20 or timed absorb | 2.25 | 2.50 / 2.55 | 125 % |

  Corrections applied to the first-cut draft (each safety-critical): LiFePO4 has
  **no float** stage — a 3.60 V/cell "float" holds the cell near-full and stresses
  it; if a rest is ever used it must be ≤3.40 V/cell (BU-409b). The Pb abort
  ceiling is raised from 2.45 (the *top of the normal* 2.30–2.45 absorb band) to
  ~2.50 V/cell so a normal absorb does not nuisance-trip. The capacity cap is made
  per-chemistry (a flat 120 % would be needlessly loose for the ~99 %-efficient
  Li-ion chemistries). The device envelope (30 V / 5 A / 150 W) additionally bounds
  every profile:
  `cells × Vcharge ≤ 30 V`, `Icharge ≤ 5 A`, `Vcharge × Icharge ≤ 150 W`.

  **NiMH is deferred out of v1** (revisit only with an external
  battery-temperature probe). On this rig NiMH cannot be made safe: the DPS-150 has
  no autonomous charge termination, and a NiMH overcharge is a *constant-voltage
  thermal* failure — voltage does not climb, so the hardware OVP never trips, the
  current is nominal so OCP never trips, and OTP is the *supply's* temperature, not
  the cell's. A backend crash mid-CC therefore has **no** hardware backstop (unlike
  Li-ion/Pb, whose crash residual is benign — the supply holds CV and the current
  tapers under HW OVP). The −ΔV signal (≈5–10 mV/cell) is also at/below the ~10 mV
  readout resolution at 2 Hz and is masked in multi-cell packs. Deferring NiMH also
  drops the −ΔV/dV/dt derivative-termination module from v1.

  Sources: Battery University [BU-403 Lead Acid](https://www.batteryuniversity.com/article/bu-403-charging-lead-acid/),
  [BU-408 NiMH](https://www.batteryuniversity.com/article/bu-408-charging-nickel-metal-hydride/),
  [BU-409 Li-ion](https://www.batteryuniversity.com/article/bu-409-charging-lithium-ion),
  [BU-409b LiFePO4](https://www.batteryuniversity.com/article/bu-409b-charging-lithium-iron-phosphate/).

### 3.8 IV curve tracer (ADR-009)

Decision: add a first-class **IV curve tracer** as a NEW independent hub
subscriber (`internal/ivtrace`, a sibling run-engine to the charge Manager and
the sequence Manager), not a charge phase and not a sequence node type. A sweep
is a **telemetry-driven step loop**, not a condition-watch: for each of N linear
steps it writes one setpoint, waits for a fresh settled telemetry tick, samples
the real operating point `(Vmeasured, Imeasured)`, and records the point — the
hardware's own CV/CC regulation means every sampled point is a true point on the
DUT's I–V curve. v1 ships **both sweep modes** (voltage and current) and rich
per-component analysis; bidirectional sweeps are deferred. This is a **low-risk**
feature — there is no battery, the worst case is a fried cheap component — so
unlike the charger it has **no pre-flight**: the output energizes at the sweep
start with the compliance already written.

- **Engine (`internal/ivtrace`)** — mirrors the charge Manager: one active
  run-slot (`Start`/`Stop`/`Run(ctx)`/`IsRunning`/`ActiveStatus`), it
  `Subscribe`s to the raw hub (~2 Hz), owns the output for the whole sweep, and
  broadcasts `device.JournalEvent`s (kind `ivProgress` during the sweep,
  `ivSweep` at the terminal). It reuses the charger's plumbing verbatim: the
  `HubController` interface (Snapshot/Subscribe/Broadcast/SetVoltage/SetCurrent/
  SetOutput/SetProtections/Refresh), `device.SafeOutputOff` + the
  telemetry-confirmed output-off check, `device.Hub.Refresh`, the staleness
  `nextTick`, the `Store` interface (adapted in `cmd/server/ivstore.go`), the
  startup reconciliation and the fail-soft session journal. Only wired when
  storage is configured (profiles/sweeps live in the DB), same as the charger.
- **Two sweep modes** (`mode: "voltage" | "current"`), both producing `(V,I)`
  points:
  - **Voltage sweep** — `SetCurrent(complianceA)` once (the current limit that
    protects the DUT), then step `Vset` linearly `vStart → vStop`. The hardware
    runs CV until the DUT's demand hits the compliance, then CC; each sampled
    point is where the DUT's curve meets the {Vset, compliance} constraint. All
    presets use this mode — the knee (where Vf/Vz/ideality live) is best resolved
    by stepping voltage.
  - **Current sweep** — `SetVoltage(complianceV)` once (the voltage ceiling), then
    step `Iset` linearly `iStart → iStop`. Better for the ohmic/high-current
    region; on a steep exponential (LED/diode) linear `Iset` steps cluster the
    `V` samples and a ceiling below Vf never lets the DUT conduct, so voltage
    sweep is preferred for the knee.
  - N linear steps (default ~50, configurable 2–1000), **uni-directional** in v1.
    The sample for a step is the first telemetry tick with
    `TS ≥ writeTS + dwellMs`, so with ~2 Hz telemetry ≥ ~0.5–1 s/step is needed to
    capture a fresh settled reading; a ~50-step sweep ≈ 30–40 s. The lamp preset
    uses a longer dwell (filament thermal settling).
- **Step loop & start order (safety invariant), each step error-checked**: (1)
  `SetProtections` (OVP/OCP/OPP/OTP from the profile, one step above the sweep
  bounds) → (2) the **compliance write** (`SetCurrent(complianceA)` for a
  V-sweep / `SetVoltage(complianceV)` for an I-sweep) → (3) the first swept
  setpoint (`SetVoltage(vStart)` / `SetCurrent(iStart)`, so the output comes on at
  the low end, typically 0) → (4) **only then** `SetOutput(true)`. The output must
  never energize before the compliance is written. Then per step: write the swept
  setpoint → drain telemetry until `TS ≥ writeTS + dwell` → sample `(V,I)` →
  record; every consumed tick is checked against the safety envelope and the
  staleness watchdog. Starting a sweep energizes the output, so
  `POST /iv/profiles/{id}/start` carries `confirm:true`, honouring the §3.5
  "enabling the output is always an explicit confirmed action" invariant.
- **Safety envelope (low-risk, non-disable-able).** (1) the **compliance** bounds
  DUT current (V-sweep) or voltage (I-sweep) — the primary DUT protection; (2)
  hardware **OVP/OCP/OPP/OTP** set from the profile a step above the sweep bounds;
  (3) **output OFF on every terminal path** via `SafeOutputOff` (fresh bounded
  context, retried, telemetry-confirmed; a failed/unconfirmed off escalates to a
  fault + alarm — the same helper the charger uses); (4) a **telemetry-staleness
  watchdog** (no tick for >~4 s → fault → SafeOutputOff — the primary trip,
  because over raw-TCP ser2net a device hang does not surface as a link-loss); (5)
  a **per-sweep hard timeout** (`steps × dwell × factor` + a floor) → abort, so a
  wedged settle loop cannot run forever; (6) the **shared interlock** (owner tag
  `iv`). The **device envelope bounds every profile**: a voltage sweep needs
  `vStop ≤ 30 V`, `complianceA ≤ 5 A`, `vStop × complianceA ≤ 150 W`; a current
  sweep needs `iStop ≤ 5 A`, `complianceV ≤ 30 V`, `complianceV × iStop ≤ 150 W`.
  There is **no pre-flight and no open-terminal read** — a passive DUT sits at 0 V
  open-circuit, so there is nothing to measure before energizing.
- **Coordination — reuses F-023's shared interlock unchanged.** The tracer, the
  charger and the sequence runner are mutually exclusive via the single
  `device.Interlock`; the tracer acquires it atomically at `Start` with owner
  `iv`. Because the 409 gate (`blockDuringInterlock`) already 409s with
  `owner+"_active"` and the automation suppressor already reads `interlock.Busy()`,
  **adding the third owner needs no change to either** — the gate emits
  `iv_active` and the suppressor covers the tracer automatically. Starting a sweep
  while a charge/sequence runs → `409 charge_active`/`409 sequence_active`;
  starting a charge/sequence while a sweep runs → `409 iv_active`.
- **Analysis (`internal/ivtrace/analyze.go`, computed on the captured dataset and
  persisted).** Least-squares fits over the sampled `(V,I)` points, per component
  type; each metric is stored on the sweep record (`metrics` JSON), annotated on
  the I(V) chart, exported and reported. The thermal voltage is
  `Vt = kT/q = 25.852 mV at 300 K` (SI-2019 exact constants; configurable per
  junction temperature — the reported `n`/`rd` assume 300 K unless overridden).
  - **LED / diode** — forward voltage **Vf** at a reference current
    (linear-interpolated; LED ref = 20 mA per datasheet convention, diode ref =
    the compliance-limited point, e.g. Vf@100 mA); **ideality factor n** from the
    Shockley equation `I = Is·(exp(V/(n·Vt)) − 1)` by fitting `ln(I)` vs `V` over
    the **mid-range exponential segment only** (slope `= 1/(n·Vt) ⇒
    n = 1/(slope·Vt)`, intercept `= ln(Is)`); **apparent series resistance Rs** as
    `dV/dI` over the top of the sweep (near compliance) — labelled *apparent*
    because it overestimates true Rs by the residual `n·Vt/I`, optionally
    corrected by subtracting it; **dynamic resistance rd = dV/dI** at a reference
    point (`= n·Vt/I + Rs`).
  - **Resistor** — **R** from a linear least-squares fit of `I` vs `V` (Ohm's law),
    with **linearity** reported as `R²` (coefficient of determination) *and* max
    deviation from the fit (`R²` alone can hide small systematic curvature).
  - **Zener** — **breakdown Vz** at the knee, at the test current **Izt**
    (interpolated), and dynamic impedance **Zzt = dV/dI** taken at Izt. The Zener
    is connected **reverse** (cathode to the + terminal) so the forward voltage
    sweep drives it into breakdown.
  - **Lamp** — **cold resistance** (`V/I` near 0 V) vs **hot resistance** (`V/I` at
    rated), and the hot/cold ratio (≈ 10–15× for tungsten's positive-
    temperature-coefficient filament).
  - **Robust fits — never fabricate a number (safety-critical for trust).** Real
    sweeps break naive least-squares, so every metric is nullable and carries a
    `quality` (`ok`/`approx`/`unreliable`) + a `reason` when absent; a missing or
    low-confidence metric is reported as such, never as a confident wrong value.
    The guards, in order:
    - **Conduction gate.** If `max(I)` never exceeds a small floor (a few × the
      noise/quantisation step above zero), the DUT did not conduct (open, reversed,
      or `vStop < Vf/Vz`) — emit `did-not-conduct` and skip every fit rather than
      fitting noise.
    - **Region selection by measured current, not the Mode flag.** The exponential
      fit uses only points with `I_min ≤ I ≤ (1−ε)·compliance`: the lower bound
      drops sub-`Vf` noise-floor points (where `ln(I)` is `NaN`/garbage), the upper
      bound drops **CC-clamped** points (a flat top that is not on the DUT curve).
      Clamping is detected from the **measured current** approaching compliance, not
      from the telemetry `Mode` field (which lags a step and misreports at the CV↔CC
      boundary).
    - **Minimum-support & conditioning guards.** A fit runs only with enough
      in-region points (ideality needs ≈8–10 log-linear points, resistor ≥3 with a
      real voltage span); it is rejected if `SS_tot ≈ 0` (degenerate — e.g. a short
      or an open reads a single clustered point) or the normal-equations are
      ill-conditioned (near-zero `x`-variance / determinant). Any of these → the
      metric is `null` with a reason, not a divide-by-tiny blow-up.
    - **Ideality `n` is `approx` by construction.** A *linear* voltage sweep places
      only a handful of steps in the exponential decade and the 12-bit measurement
      quantises `ln(I)`, so `n` is a best-effort estimate labelled `approx` (with
      the in-region point count) — a finer adaptive step near the knee is a
      documented v2 improvement, not a v1 promise of laboratory accuracy.
    - **Zener/knee "reached" gate.** `Vz`/`Zzt` are emitted only if the sweep
      actually entered breakdown (current rose through `Izt` before `vStop`);
      otherwise `breakdown-not-reached`, never an extrapolated `Vz`.
    - Only the component's configured analyses run (an ideality fit is never
      attempted on a resistor). These fit functions are unit-tested **directly**
      against crafted arrays — noisy, quantised, CC-clamped, non-conducting and
      single-point-degenerate — not only against the ideal emulator, because the
      emulator's instant, noiseless settling hides exactly the cases these guards
      exist for.
- **Storage, reconciliation & UI.** Two feature-owned models registered through
  `storage.Config.Models`: `iv_profiles` (saved CRUD profiles) and `iv_sweeps`
  (one row per run, created `running` at start and finalized at the terminal
  state, carrying the full `points` array, the computed `metrics`, and a
  `snapshot` of bounds/compliance/protections). Unix-milli int64 times, opaque
  JSON string columns — the charge-storage convention. **Startup reconciliation**
  finalizes any `iv_sweeps` row left `running` by a crash as `failed` and cuts a
  stray energized output with no owner (the charger's `reconcileOnBoot`/
  `cutStrayOutput` pattern). CSV export of a sweep's point dataset. A new **IV /
  ВАХ** page (like the Charge page): profiles CRUD; a live sweep (the curve builds
  in real time from `ivProgress` with a step-progress indicator); a new **I(V)
  chart** (uPlot with V on the x-axis, I on the y-axis — not a time series —
  showing the compliance band and the annotated metrics); sweep history and CSV
  export.
- **Emulator DUT.** `internal/device/emulator` gains a passive **device-under-test**
  on the terminals (a sibling to the F-023 `battery`) so a sweep is testable
  end-to-end on `mock://`. `WithDUT(DUTConfig)` + `DPS_MOCK_DUT` (mirroring
  `WithBattery`/`DPS_MOCK_BATTERY`) attach either a **resistor** (`I = V/R`), a
  **diode/LED** (Shockley `I = Is·(exp(V/(n·Vt)) − 1)` with a series `Rs`), or a
  **zener** (the diode branch plus a **reverse-breakdown** term that clamps at
  `Vz` with dynamic impedance `Zzt` once the forward-swept terminal reaches the
  knee) — the last so the Zener preset and its `Vz@Izt`/`Zzt` analysis are
  exercised end-to-end on `mock://`, not only in direct fit unit-tests. Given
  the supply's `{vset, iset}` regulation the DUT returns the self-consistent
  operating point (CV at `vset` when the DUT's demand ≤ `iset`, else CC at `iset`
  with the terminal voltage the DUT needs for that current — for the diode found
  by bisection on the monotonic Shockley curve). Unlike the battery the DUT is
  **stateless**: it reads `0 V` open-circuit with the output off (no pre-flight)
  and integrates no charge (no `chargeStep`), so only `measure()`/`currentMode()`
  gain a `dut` branch. A DUT and a battery are mutually exclusive (both claim the
  terminals); a DUT is ignored with a warning if a battery is also configured.
- **Component presets (validated — cite sources).** All voltage-sweep. Compliance
  and bounds are first-class safety parameters bounded by the device envelope.

  | Component | Sweep (V) | Compliance | Steps | Dwell | Analysis |
  |---|---|---|---|---|---|
  | LED | 0 → 6 | 20 mA | ~50 | ~1 s | Vf@20 mA, n, apparent Rs, rd |
  | Diode 1N400x | 0 → 1 | 100 mA | ~50 | ~1 s | Vf@100 mA (knee), n, apparent Rs, rd |
  | Zener | 0 → Vz + ~20 % | derived: `min(Izt≈5 mA, derate·Pmax/Vz)` | ~50 | ~1 s | Vz@Izt, Zzt |
  | Resistor | 0 → `√(derate·P·R)` | `Vmax/R` | ~50 | ~1 s | R, R², max-dev % |
  | Lamp | 0 → rated | ~1.5 × rated I (inrush) | ~50 | ~2 s | R_cold, R_hot, ratio |

  Corrections applied to the first-cut draft: the **LED ceiling is raised 4 → ~6 V**
  — 4 V is marginal for violet/UV and high-Vf white dies (3.6–4.2 V at the die)
  and leaves no drive headroom to actually reach 20 mA; 20 mA compliance is kept
  (the standard indicator-LED test current, and it protects low-Vf reds). The
  **Zener compliance is derived from the part's power rating, not a fixed 15 mA**
  (safety-critical): a constant 15 mA is fine at low Vz (12 V × 15 mA = 180 mW)
  but exceeds a 500 mW part above ~33 V, so
  `complianceA = min(nominal Izt, derate × Pmax / Vz)` with the read taken near the
  true **Izt ≈ 5 mA** for a 500 mW BZX55/BZX79 glass Zener (the 1N47xx series is
  **1 W**, not 500 mW, with device-specific higher Izt — do not lump them). The
  **resistor Vmax is derated** to `√(derate·P·R)` (≈ 50 %) so a small-R part stays
  under its power rating (a ¼ W 100 Ω part ⇒ Vmax ≈ 3.5 V), with
  `complianceA = Vmax/R`. `Rs` is reported as **apparent Rs** (overestimated by
  `n·Vt/I`) and the ideality fit uses only the mid-range exponential segment
  (low-current recombination inflates `n` toward 2; high-current `Rs` inflates it
  too).

  Sources: [PVEducation — Diode Equation](https://www.pveducation.org/pvcdrom/pn-junctions/diode-equation)
  (Shockley, ideality, `rd = n·Vt/I`);
  [Boltzmann constant](https://en.wikipedia.org/wiki/Boltzmann_constant) /
  [Elementary charge](https://en.wikipedia.org/wiki/Elementary_charge) /
  [NIST SP 330](https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.330-2019.pdf)
  (SI-2019 exact constants, `Vt = 25.852 mV @ 300 K`);
  [onsemi 1N4001-07](https://www.onsemi.com/download/data-sheet/pdf/1n4001-d.pdf)
  (1 A rating, Vf 1.1 V @ 1 A);
  [LED Vf @ 20 mA](https://industrialmonitordirect.com/blogs/knowledgebase/led-forward-voltage-current-limiting-and-datasheet-specs);
  [Vishay BZX55](https://www.vishay.com/docs/85604/bzx55.pdf) (Izt = 5 mA, Zzt);
  [onsemi 1N4728A (1 W)](https://www.mouser.com/datasheet/2/149/1N4728A-196207.pdf);
  [coefficient of determination](https://en.wikipedia.org/wiki/Coefficient_of_determination);
  [Ohm's law / P=V²/R](https://en.wikipedia.org/wiki/Ohm%27s_law); tungsten lamp
  cold/hot ratio ([BCcampus](https://pressbooks.bccampus.ca/lightingforelectricians/chapter/incandescent/)).

### 3.9 IV sweep comparison + component library (ADR-010)

Decision: add two read-only features on top of the F-024 tracer — a **comparison /
overlay** of recorded IV sweeps and a **library of characterized physical
components** — with **zero device, run-engine, interlock, protocol, safety or
emulator surface**. This is the ADR's main structural point: F-025 never writes a
setpoint, never energizes the output, never acquires the shared `device.Interlock`
and never touches `internal/ivtrace` or the emulator, so **by construction it
cannot affect output safety**. It is one new storage entity, one additive column,
a handful of read/CRUD endpoints and two new frontend tabs; the frozen v5 start
path (`POST /iv/profiles/{id}/start`, the `ivtrace` step loop, its `Request`/
`Plan`/`RunStatus`) is untouched. The only new mutation is a column write on an
already-finalized sweep row.

- **Rejected — solar / PV cell characterization.** A photovoltaic I–V curve
  (Pmax, Vmp, Imp, fill-factor) is traced by sweeping an *illuminated* cell as it
  **delivers** power into a variable load — the fourth, power-generating quadrant
  (V > 0, I < 0). The DPS-150 is a single-quadrant **source**: it can only push
  current out, never sink the cell's photocurrent, so that quadrant is
  **physically unreachable** on this rig and PV analysis is infeasible — dropped
  from scope. (A *dark* forward sweep of a PV cell is possible, but that is just
  the existing F-024 diode analysis and needs no new feature.)

- **Data model — a new first-class entity (decision A).** A component exists
  **independently of any sweep**. A new `iv_components` table
  `{id, name, kind, part_number, notes, ref_sweep_id, created_at, updated_at}`:
  `kind` reuses the F-024 component enum (`led|diode|zener|resistor|lamp|generic`),
  **fixed at creation** (so the ref-sweep type invariant below cannot be broken by
  an edit); `name` size mirrors the profile (≤ 200); `notes` is free text;
  `part_number` optional. `iv_sweeps` gains a **nullable, indexed `component_id`**
  column — `0`/`NULL` = unassigned, so **every existing sweep stays unassigned and
  the change is backward-compatible**. The migration is **additive**: the new model
  and the new column auto-migrate on both sqlite and postgres via
  `storage.Config.Models` (a new anchor for `iv_components`; AutoMigrate adds the
  `component_id` column + index in place) — no dialect functions, no separate SQL
  migration, the charge/IV storage convention (§3.7/§3.8).

- **Association is post-hoc only (decision b).** A finished sweep is assigned to a
  component **after the fact**, from the history/detail view, via a single new
  sweep mutation (`POST /iv/sweeps/{id}/component`). The run engine and the v5
  **start path carry no `component_id`** — there is **no start-time preselect** in
  v1 (explicitly a v2 deferral), so `ivtrace.Request`/`Plan`/`RunStatus` and the
  whole run loop are unchanged. Assignment requires the sweep's F-024 `component`
  type to **equal the component's `kind`** (a resistor sweep cannot join an LED
  component) — this keeps the ref-curve type invariant intact even for the
  first-assigned sweep, which becomes the default reference.

- **Reference curve — an explicit pin (decision i).** The component carries an
  explicit `ref_sweep_id` naming its **canonical characterization**; the
  component's displayed metrics are *that sweep's* stored `metrics`, **not
  recomputed** — the library shows the same numbers the sweep detail shows, and all
  analysis stays owned by `ivtrace` (§3.8). The default `ref_sweep_id` is the
  **first sweep assigned**; the user can **re-pin** any of the component's sweeps
  via `PUT /iv/components/{id}`. A pin is **validated**: the sweep must **exist**,
  carry this component's `component_id`, **and** its stored `component` must
  **match the component's `kind`** — otherwise `400 invalid_iv_component`. The pin
  **never dangles**: whenever the pinned sweep stops being a member of the
  component (it is unassigned, reassigned to another component, or deleted),
  `ref_sweep_id` **auto-reassigns to the newest remaining `completed` member** (by
  `started_at`, then id), or `NULL` when none remain. Centralized in one repo
  method reused by every path that changes membership.

- **Data-integrity invariants (design review — no real FK constraints, so app
  code enforces them).** GORM AutoMigrate does not add reliable cross-dialect
  foreign keys, so every referential rule is enforced in the repo, each membership
  mutation in **one DB transaction** (the `component_id` write and the ref-fixup
  are atomic — a mid-write failure can never leave a reassigned sweep with a
  dangling old ref):
  - **The ref-fixup is a single shared invariant on *every* write that changes a
    sweep's `component_id`** — assign, unassign, reassign A→B, component delete,
    **and sweep delete** — not just one of them. Whenever a sweep leaves a
    component that had pinned it, that component re-pins per the auto-reassign rule
    above, in the same transaction.
  - **Only `completed` sweeps are assignable and ref-eligible.** A `running`/
    `aborted`/`failed` sweep has empty or truncated points and would render a
    garbage reference curve, so `POST /iv/sweeps/{id}/component` rejects a
    non-`completed` sweep (`400 invalid_iv_component`) and the auto-default/
    auto-reassign only ever pick a `completed` member.
  - **Sweep deletion exists and routes through the fixup.** F-025 adds
    `DELETE /iv/sweeps/{id}` (library pruning of junk/duplicate sweeps — the
    deletion the grill's "auto-reassign on delete" presupposed); it runs the same
    transactional ref-fixup, and any Сравнение URL still holding the id drops it
    silently (below). Deleting a component still only **unassigns** its sweeps
    (never deletes them).
  - **`kind` matching, with `generic` as a wildcard.** Assignment/re-pin require
    the sweep's F-024 `component` to equal the component's `kind`, **except a
    `generic` component accepts a sweep of any type** (its metrics table simply
    won't line up). A specific-kind component never accepts a mismatched or
    `generic` sweep.
  - **`component_id` uses the codebase int64-zero convention** (`0` = unassigned,
    like `ProfileID`), but the **`?componentId=` filter requires a positive
    integer** (`≤ 0`/absent ⇒ no filter, never a match on the `0` rows), and the
    filter predicate is applied to **both** the `Count(total)` and the paged
    `Find` (a filter on only the page query would return the right rows with a
    global `total`, paginating to empty pages). `sweepCount` is one
    `GROUP BY component_id` aggregate, not a per-component count (no N+1).

- **Comparison is frontend-only — no backend comparison endpoint (decision).** The
  core is a client-side **overlay of a set of sweep-ids** on one I(V) uPlot: each
  sweep's full point set comes from the existing `GET /iv/sweeps/{id}` (the
  authoritative points F-024 already persists), and the metrics table is assembled
  in the browser from the same responses. The backend adds **no aggregate/compare
  route** — comparison is pure presentation. Three entry points build the sweep-id
  set: **(x)** arbitrary sweeps **multi-selected from История**; **(y)** the
  reference curves of **N components** picked in the Библиотека (each resolved to
  its `ref_sweep_id`); **(z)** **all sweeps of one component**
  (`GET /iv/sweeps?componentId=X`).

- **Comparison view content.** The **overlay chart always renders** for any
  non-empty set — including a mixed set of component types *and* of voltage/current
  sweep modes, since every sweep is just a series of `(V,I)` points on shared axes
  — with a **legend carrying a per-curve show/hide toggle**. A side-by-side
  **metrics table** (one column per sweep, each row annotated with **min / max /
  spread**) renders **only when every selected sweep shares one `component`
  type** (their metric keys line up); for a **mixed-type** set the table is
  **hidden with a hint** ("select one component type to compare metrics"), because
  a resistor's `resistance` and a diode's `ideality` are not comparable rows. The
  per-row **min / max / spread ignore `null` metrics** (F-024 metrics are
  `number | null`) — a row with fewer than two non-null values renders `—`, never
  `NaN`; `generic` sweeps carry no typed metrics and are treated as
  not-comparable.

- **Rendering.** Curves are drawn **raw `(V,I)`, no normalization**, both axes
  **auto-fit to the union** of the selected sweeps. The **compliance band is
  dropped in the overlay** (it is per-sweep and meaningless across a mixed set — it
  stays only in the single-sweep `IVChart`). The overlay is **capped at ~8 curves**
  with a clear message when the set is larger (a qualitative palette saturates and
  the chart turns to spaghetti past ~8); the palette is **theme-aware /
  qualitative**. A **lin/log Y-axis toggle** ships in v1 and applies to **both** the
  overlay and the single-sweep view — in log mode `I ≤ 0` points (the
  sub-conduction noise floor and any negative reading) are **skipped/clamped** so
  the log scale stays well-defined. Each series is **sorted by `V` ascending
  before line rendering** — a *current*-sweep's `(V,I)` points are not monotonic in
  `V`, so an unsorted line series zigzags across the plot. The **~8-curve cap is
  applied in the loader**, not the renderer: the `?ids=` list is deduped and
  validated first, then the **first 8 distinct valid ids in URL order** are
  fetched — so a pasted 500-id URL never fires 500 sequential
  `GET /iv/sweeps/{id}` requests.

- **UI — two new tabs inside the existing ВАХ page, no new route.** The page's tab
  set grows to five — **Live / Профили / История / Библиотека / Сравнение** — all
  `?tab=`-driven via the History-API `replaceState` pattern already used for
  `?range=`/`?tab=` (bookmarkable, survives reload). The **Сравнение** tab reads its
  sweep-id set **from the URL** (`?tab=compare&ids=1,2,3`); the URL is the **single
  source of truth** for the selection (the three entry points navigate here by
  writing `ids`), it is bookmarkable/shareable, and it shows an **empty-state** when
  no ids are present. A **stale / deleted / non-numeric id is skipped silently**
  with a small note ("2 sweeps no longer exist"), never a crash — the client
  fetches each `GET /iv/sweeps/{id}` and drops the misses.

- **Export — client-side long-format CSV.** A button in the Сравнение tab generates
  a **long-format comparison CSV in the browser** from the already-fetched points:
  columns `sweepId,label,index,voltage,current,power` (one row per point per sweep,
  `power = voltage × current`). The per-sweep `GET /iv/sweeps/{id}.csv` (F-024,
  single/wide) is **unchanged**, and there is **no component-level export** in v1.
  The `label` is user-controlled (a sweep/component name), so the client CSV writer
  **quotes every field and neutralizes a leading `= + - @`** (prefix with `'`) to
  prevent spreadsheet formula injection — the backend per-sweep CSV is number-only
  and already safe.

- **Migration plan (phased, each phase backward-compatible).**
  1. **Contract freeze** — this ADR + contract v6 land in one MR; no code yet.
  2. **Storage** — the `iv_components` model + the nullable indexed `component_id`
     on `IVSweep`, registered through `storage.Config.Models`; repo methods
     (component CRUD, sweep assign/unassign/**delete** — each in **one
     transaction** with the shared ref-fixup — the completed-only + kind-match
     checks, the count-filtered `?componentId=` sweeps list, the ref-pin
     validation, and `sweepCount` via one `GROUP BY`). AutoMigrate adds the
     table/column on an existing DB with no data change — all prior sweeps read
     back unassigned.
  3. **API** — the `/iv/components` CRUD handlers, the `POST /iv/sweeps/{id}/component`
     mutation, the `DELETE /iv/sweeps/{id}` handler, the `componentId` filter/
     `componentId` field on the sweeps routes, wired in `internal/api/iv.go` +
     `registerIVRoutes`; all 503 when storage is off, matching F-024.
  4. **Frontend** — the Библиотека and Сравнение tabs, the multi-series overlay
     chart with the lin/log toggle and no compliance band, the metrics + min/max/
     spread table, the client-side long-CSV, the `src/api/iv.ts` extensions, MSW
     mocks and `iv.*` i18n keys.
  5. **Docs / deploy** — CHANGELOG + this doc; release is an MR bumping
     `image.tag` (§4). No new env, secret, config flag or infra change.

## 4. Deploy and environments

- **Single environment** (ns `dps150`): a second instance could not connect
  to the single-client ser2net; e2e and development run on the emulator.
- GitLab CI (`git.example.com`, registry `:5005`): lint → test → build;
  CI only builds and pushes images (:short-sha + :latest on master).
- Deploy — GitOps via ArgoCD (ADR-005): the chart lives in
  `infrastructure/argocd-platform` `apps/dps150/`, a release = an MR bumping
  `image.tag`; the ApplicationSet creates the namespace with PSA labels, selfHeal.
- Cluster facts: dnsDomain `k8s.example.com` (not cluster.local!), ingress-class
  `traefik`, cert-manager ClusterIssuer `letsencrypt-cloudflare`,
  external-dns → Cloudflare, storage `longhorn` (not needed for the DB — the DB is in CNPG).
- Local run: binary (serial+SQLite), docker-compose
  (backend+frontend+postgres, for docker/portainer/orbstack).

## 5. User and scenarios (CJM)

The sole user is the owner. Devices: desktop at the workbench
+ phone from another room.

| Scenario | Path | What is critical |
|---|---|---|
| Quickly power a circuit | open dashboard → profile or setpoints → output ON → watch consumption | speed, large numbers, 1 click to a profile |
| Long run | set up → walk away → monitor from the phone → Telegram on a protection trip | charts, events, notifications, mobile screen |
| Battery charge/test | charge profile → watch Ah/Wh → (Stage 3) auto-stop on a condition | D8/D9/DA counters, auto-stop rules, Telegram |
| Repair/diagnostics | profile with strict current limits → apply power → instantly see CC/trip | protections in the profile, fast UI reaction to events |

## 6. Stages (= GitLab Milestones)

- **Stage 1 "PoC"**: scaffold (Go+React+CI+Docker+Helm), protocol library
  + emulator + serial/tcp/mock transports, minimal web (live telemetry,
  V/I, output), SQLite locally, ser2net on pve, DB in CNPG, Authelia,
  auto-deploy to the cluster. Result: the service lives in the cluster and controls the PSU.
- **Stage 2 "MVP"**: profiles + M1–M6 sync, month-long history + charts,
  protection setpoints + event journal, Telegram notifications, mobile layout,
  Ah/Wh counters.
- **Stage 3 "v1.0"**: auto-stop rules (current < X for longer than Y min / after Z Ah/Wh /
  after T hours; hardware protections as a backstop), CSV export, API tokens,
  publishing a mirror on GitHub.

## 7. Open items

- The exact serial-device name on pve (`/dev/serial/by-id/…`) — to be learned when
  the PSU is connected; ser2net is configured over SSH (root@10.20.0.5) with
  confirmation before changes.
- Managed role + Database CR + VSS for dps150 are added to the
  `k8s-talos-cluster` repo (helm/pgsql-cluster) — a separate MR there.
- CI variable `KCONFIG` — check that it exists at the applications group level,
  otherwise copy it from the cattery project.
