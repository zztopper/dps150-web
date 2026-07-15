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
  output toggles — who/when/what), `settings`, (Stage 3) `automation_rules`.
- Volume: ~5.2 M rows/month — time-based partitions/indexes, no TSDB.

### 3.5 Security

- Ingress `dps150.example.com` entirely behind Authelia forward-auth
  (middleware `authelia-forwardauth-authelia@kubernetescrd`), cluster SSO.
- ser2net on pve: listens only for the cluster subnet (firewall/bind).
- Secrets (PG, Telegram token) — Vault → VaultStaticSecret → k8s Secret.
- Applying a profile NEVER enables the output automatically.
  Enabling the output is only an explicit action with confirmation in the UI.
- (Stage 3) API tokens for scripted access.

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
