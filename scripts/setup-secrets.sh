#!/usr/bin/env bash
# setup-secrets.sh — авторизовать glab/gh CLI на основе .env.
#
# Поведение:
#   1. Загружает .env (рядом с проектом).
#   2. Если ISSUE_PROVIDER=gitlab или задан GITLAB_TOKEN — авторизует glab.
#   3. Если ISSUE_PROVIDER=github или задан GITHUB_TOKEN — авторизует gh.
#   4. Не пишет токены наружу, использует stdin.
#
# Использование:
#   ./scripts/setup-secrets.sh
#
# Требования: glab и/или gh должны быть установлены (см. SETUP.md).

set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ ! -f ".env" ]; then
    echo "ERROR: .env не найден. Скопируйте .env.example -> .env и заполните." >&2
    exit 1
fi

# Загружаем переменные, не выводя их в shell history.
set -o allexport
# shellcheck disable=SC1091
source .env
set +o allexport

PROVIDER="${ISSUE_PROVIDER:-}"
CONFIGURED=0

# --- GitLab ---
if [ "$PROVIDER" = "gitlab" ] || [ -n "${GITLAB_TOKEN:-}" ]; then
    if ! command -v glab >/dev/null 2>&1; then
        echo "WARN: glab не установлен. macOS: brew install glab. См. SETUP.md." >&2
    elif [ -z "${GITLAB_TOKEN:-}" ]; then
        echo "WARN: GITLAB_TOKEN не задан в .env — пропускаю настройку glab." >&2
    else
        HOST="${GITLAB_HOST:-gitlab.com}"
        echo "Настраиваю glab для $HOST..."
        # glab принимает токен через stdin при --stdin.
        echo "$GITLAB_TOKEN" | glab auth login --hostname "$HOST" --stdin
        # Делаем хост дефолтным, чтобы --repo был коротким.
        glab config set -g host "$HOST" 2>/dev/null || true
        echo "OK: glab авторизован для $HOST."
        CONFIGURED=1
    fi
fi

# --- GitHub ---
if [ "$PROVIDER" = "github" ] || [ -n "${GITHUB_TOKEN:-}" ]; then
    if ! command -v gh >/dev/null 2>&1; then
        echo "WARN: gh не установлен. macOS: brew install gh. См. SETUP.md." >&2
    elif [ -z "${GITHUB_TOKEN:-}" ]; then
        echo "WARN: GITHUB_TOKEN не задан в .env — пропускаю настройку gh." >&2
    else
        echo "Настраиваю gh для github.com..."
        echo "$GITHUB_TOKEN" | gh auth login --with-token
        echo "OK: gh авторизован."
        CONFIGURED=1
    fi
fi

if [ $CONFIGURED -eq 0 ]; then
    echo "Ничего не настроено. Заполните GITLAB_TOKEN и/или GITHUB_TOKEN в .env." >&2
    exit 1
fi

echo ""
echo "Готово. Проверьте:"
[ "$PROVIDER" = "gitlab" ] && echo "  glab auth status"
[ "$PROVIDER" = "github" ] && echo "  gh auth status"
