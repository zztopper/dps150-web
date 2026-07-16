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
