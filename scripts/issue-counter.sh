#!/usr/bin/env bash
# issue-counter.sh — получить следующий номер для нового Issue по префиксу.
#
# Префиксы (см. docs/issue-conventions.md):
#   F  — feature
#   B  — bug
#   TD — tech-debt
#   S  — security
#   D  — documentation
#   I  — infra
#
# Получает все Issues (открытые и закрытые), вытаскивает заголовки вида
# "PREFIX-NNN: ...", находит максимальный номер, возвращает следующий.
#
# Использование:
#   ./scripts/issue-counter.sh F        # -> 042 (например)
#   ./scripts/issue-counter.sh TD       # -> 008
#
# Требует .env с ISSUE_PROVIDER + glab/gh настроенными (см. setup-secrets.sh).

set -euo pipefail

PREFIX="${1:?Укажите префикс: F | B | TD | S | D | I}"

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ -f ".env" ]; then
    set -o allexport
    # shellcheck disable=SC1091
    source .env
    set +o allexport
fi

PROVIDER="${ISSUE_PROVIDER:-gitlab}"

extract_max() {
    awk -v p="^${PREFIX}-" '
        match($0, p "[0-9]+") {
            n = substr($0, RSTART + length(p) - 1, RLENGTH - length(p) + 1)
            if (n + 0 > max) max = n + 0
        }
        END { print (max ? max : 0) }
    '
}

case "$PROVIDER" in
    gitlab)
        REPO="${GITLAB_GROUP:?}/${GITLAB_PROJECT:?}"
        # Получаем все issues (open + closed), только заголовки. -P 100 — макс. на страницу.
        TITLES=$(glab issue list --repo "$REPO" --all -P 100 -O json 2>/dev/null \
                 | jq -r '.[].title' || true)
        ;;
    github)
        REPO="${GITHUB_REPO:?}"
        TITLES=$(gh issue list --repo "$REPO" --state all --limit 1000 \
                 --json title --jq '.[].title' 2>/dev/null || true)
        ;;
    *)
        echo "ERROR: ISSUE_PROVIDER=$PROVIDER (ожидался gitlab или github)" >&2
        exit 1
        ;;
esac

MAX=$(printf '%s\n' "$TITLES" | extract_max)
NEXT=$((MAX + 1))
printf "%s-%03d\n" "$PREFIX" "$NEXT"
