#!/usr/bin/env bash
# CI-only entry: allowlisted env, test binary (never cmd/server), loopback netns.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d "${RUNNER_TEMP:-/tmp}/dps-e2e.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir "$work/home"
# A fresh HOME prevents ambient DB, npm, git and cloud config from entering.
env -i PATH="$PATH" HOME="$work/home" CI=1 E2E_WORK="$work" E2E_ROOT="$root" bash --noprofile --norc <<'ISOLATED'
set -euo pipefail
cd "$E2E_ROOT/backend"
go version
CGO_ENABLED=0 go test -c -tags isolated_e2e -o "$E2E_WORK/api.test" ./internal/api
cd "$E2E_ROOT/frontend"
npm ci --prefer-offline
npx playwright install chromium
# Fail closed if the runner cannot create a network namespace or set up lo.
# Dependency downloads finish before the HTTP fixture/browser start.
unshare --user --map-root-user --net bash --noprofile --norc <<'NETWORK'
set -euo pipefail
ip link set lo up
export E2E_API_TEST_BINARY="$E2E_WORK/api.test"
npm run e2e
NETWORK
ISOLATED
