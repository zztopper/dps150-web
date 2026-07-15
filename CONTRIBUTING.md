# Contributing to dps150-web

Thanks for your interest in improving dps150-web! This guide covers the
essentials for getting a change merged.

## Getting started

Requirements: **Go 1.25+**, **Node.js 20+**, and `golangci-lint` for linting.

```bash
make build   # backend binary (frontend embedded) + frontend bundle
make run     # run the backend on :8080 against the device emulator (mock://)
```

You do **not** need a physical DPS-150 to develop: the `mock://` transport is a
frame-level emulator that the backend, tests and e2e all run against.

## Project structure

| Path | What lives here |
|---|---|
| `backend/` | Go backend — device driver, transports, hub, REST API, WebSocket, storage, notifier |
| `frontend/` | React 19 + TypeScript SPA (Vite, Ant Design, TanStack Query, uPlot) |
| `docs/` | Design doc, API contract, runbooks |
| `docs/FNIRSI_DPS-150_Protocol.md` | Vendored protocol reference (community reverse-engineering, MIT) — the source of truth for the device protocol |

## Before you open a pull request

Run the full local gate — CI runs the same checks:

```bash
make lint    # gofmt + go vet + golangci-lint, oxlint + tsc -b
make test    # go test + vitest
make build   # must build cleanly
```

For changes that touch the UI or the request flow, also run the e2e suite:

```bash
cd backend && go build -o bin/dps150-server ./cmd/server
cd frontend && npm run e2e
```

Keep changes focused: update tests alongside code, and keep the frontend types
and backend API contract in sync when you change an endpoint.

## Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <summary>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`. For
example:

```
feat(profiles): assign a profile to a hardware memory cell
fix(hub): pace writes to avoid dropped DPS-150 commands
```

Note anything user-visible in `CHANGELOG.md` under `[Unreleased]`
([Keep a Changelog](https://keepachangelog.com/) format).

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
