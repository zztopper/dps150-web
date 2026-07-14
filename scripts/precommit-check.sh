#!/usr/bin/env bash
# precommit-check.sh — запуск автоматических проверок перед коммитом.
#
# Покрывает шаги A-D из docs/pre-commit-gate.md:
#   A. Сборка backend, frontend, опц. docker-образов.
#   B. Линтеры и форматтеры.
#   C. Пирамида тестов: unit + coverage gate, integration, E2E.
#   D. LikeC4 validate.
#
# Шаги E-H (соответствие ТЗ, Devil's Advocate, ADR, Issue/ветка) —
# содержательные, проверяются разработчиком/оркестрирующим агентом, не скриптом.
#
# Использование:
#   ./scripts/precommit-check.sh            # обычный набор (A,B,C-unit,C-int,D)
#   ./scripts/precommit-check.sh --quick    # без E2E и docker build
#   ./scripts/precommit-check.sh --full     # + docker build + security audit
#   ./scripts/precommit-check.sh --skip e2e,docker
#
# Скрипт намеренно вызывает make-цели, которые принято иметь в проекте:
#   make lint, make build, make build-docker, make test, make test-int, make test-e2e
# Если у вас нет такой цели — добавьте или адаптируйте секцию ниже.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MODE="normal"
SKIP_LIST=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --quick)  MODE="quick";  shift ;;
        --full)   MODE="full";   shift ;;
        --skip)   SKIP_LIST="$2"; shift 2 ;;
        --help)
            sed -n '1,30p' "$0"; exit 0 ;;
        *)
            echo "Неизвестный аргумент: $1"; exit 2 ;;
    esac
done

skip() {
    local what="$1"
    if [[ ",${SKIP_LIST}," == *",${what},"* ]]; then
        return 0
    fi
    if [ "$MODE" = "quick" ] && [[ "$what" == "e2e" || "$what" == "docker" ]]; then
        return 0
    fi
    return 1
}

run() {
    local desc="$1"; shift
    echo ""
    echo "===== $desc ====="
    "$@"
}

failures=()

# Не падаем сразу — собираем все ошибки и в конце выводим итог.
try() {
    local desc="$1"; shift
    if ! run "$desc" "$@"; then
        failures+=("$desc")
    fi
}

# ---------- A. Сборка ----------
if [ -f "Makefile" ] && grep -q '^build:' Makefile; then
    try "A1. make build" make build
fi

if [ -f "frontend/package.json" ]; then
    try "A2. frontend build" bash -c 'cd frontend && (npm run build 2>/dev/null || npx vite build)'
fi

if ! skip docker; then
    if [ -f "Makefile" ] && grep -q '^build-docker:' Makefile; then
        try "A3. make build-docker" make build-docker
    fi
fi

# ---------- B. Линтеры / формат ----------
if [ -f "Makefile" ] && grep -q '^lint:' Makefile; then
    try "B1. make lint" make lint
fi

if [ -d "backend" ] && command -v gofmt >/dev/null; then
    try "B2. gofmt check" bash -c 'diff -u <(echo -n) <(gofmt -l backend/)'
fi

if [ -f "frontend/package.json" ]; then
    try "B3. frontend type-check" bash -c 'cd frontend && npx tsc -b'
fi

# ---------- C. Тесты ----------
if [ -f "Makefile" ] && grep -q '^test:' Makefile; then
    try "C1. make test (unit)" make test
fi

# Coverage gate ≥ 85% (best-effort: проверяем, если есть coverage.out)
if [ -f "backend/coverage.out" ]; then
    pct=$(go tool cover -func=backend/coverage.out 2>/dev/null \
          | awk '/^total:/ {gsub("%","",$3); print $3}')
    if [ -n "${pct:-}" ]; then
        echo "Backend total coverage: ${pct}%"
        awk -v p="$pct" 'BEGIN{ if (p+0 < 85) exit 1 }' \
            || failures+=("C1.1 backend coverage < 85% (got ${pct}%)")
    fi
fi

if [ -f "Makefile" ] && grep -q '^test-int:' Makefile; then
    try "C2. make test-int (integration)" make test-int
fi

if ! skip e2e; then
    if [ -f "Makefile" ] && grep -q '^test-e2e:' Makefile; then
        try "C3. make test-e2e" make test-e2e
    fi
fi

# ---------- D. LikeC4 ----------
if [ -d "docs/architecture/likec4" ]; then
    try "D1. likec4 validate" npx -y likec4 validate docs/architecture/likec4
fi

# ---------- Опциональные расширения --full ----------
if [ "$MODE" = "full" ]; then
    # Vulnerability scans
    if [ -d "backend" ] && command -v govulncheck >/dev/null; then
        try "F1. govulncheck" bash -c 'cd backend && govulncheck ./...'
    fi
    if [ -f "frontend/package.json" ]; then
        try "F2. npm audit (high+)" bash -c 'cd frontend && npm audit --omit=dev --audit-level=high'
    fi

    # Secret scanning
    if command -v gitleaks >/dev/null; then
        try "F3. gitleaks (secrets)" gitleaks detect --no-git --redact -v
    fi

    # SBOM generation (CycloneDX)
    if [ -d "backend" ] && command -v cyclonedx-gomod >/dev/null; then
        try "F4. SBOM backend" bash -c 'cd backend && cyclonedx-gomod mod -output ../sbom-backend.json -json'
    fi
    if [ -f "frontend/package.json" ] && command -v cyclonedx-npm >/dev/null; then
        try "F5. SBOM frontend" bash -c 'cd frontend && cyclonedx-npm --output-file ../sbom-frontend.json'
    fi

    # License compliance
    if [ -d "backend" ] && command -v go-licenses >/dev/null; then
        try "F6. license check backend" bash -c 'cd backend && go-licenses check ./... --disallowed_types=forbidden,restricted'
    fi
    if [ -f "frontend/package.json" ] && command -v license-checker >/dev/null; then
        try "F7. license check frontend" bash -c 'cd frontend && license-checker --production --onlyAllow="MIT;Apache-2.0;BSD-2-Clause;BSD-3-Clause;ISC;MPL-2.0;CC0-1.0;Unlicense"'
    fi

    # Container scanning
    if command -v trivy >/dev/null && [ -f "Dockerfile" ]; then
        try "F8. trivy filesystem scan" trivy fs --severity HIGH,CRITICAL --exit-code 1 .
    fi
fi

# ---------- Итог ----------
echo ""
echo "================================================"
if [ ${#failures[@]} -eq 0 ]; then
    echo "✓ Все автоматические проверки прошли."
    echo "  Дальше — содержательные шаги (E-H в docs/pre-commit-gate.md):"
    echo "    E. Соответствие ТЗ (Issue + AC)."
    echo "    F. ADR обновлён, если архитектурное решение принято."
    echo "    G. Devil's Advocate провёл ревью и все находки устранены."
    echo "    H. Issue и ветка корректно оформлены."
    exit 0
else
    echo "✗ FAIL: следующие проверки упали:"
    for f in "${failures[@]}"; do
        echo "  - $f"
    done
    echo ""
    echo "Коммит запрещён. Исправьте и перезапустите."
    exit 1
fi
