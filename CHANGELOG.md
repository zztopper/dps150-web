# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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
