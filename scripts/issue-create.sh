#!/usr/bin/env bash
# issue-create.sh — создать Issue в GitLab/GitHub с правильным префиксом и labels.
#
# Использование:
#   ./scripts/issue-create.sh F "Поддержка экспорта в PDF" feature,frontend
#   ./scripts/issue-create.sh B "Падает форма регистрации"  bug,frontend
#   ./scripts/issue-create.sh TD "Удалить дублирующиеся хелперы" tech-debt
#
# Аргументы:
#   $1 — префикс (F | B | TD | S | D | I)
#   $2 — заголовок (без префикса)
#   $3 — labels через запятую (опционально)
#   $4 — путь к файлу с описанием в Markdown (опционально, иначе stdin или пустое)
#
# Описание Issue должно содержать RICE-таблицу — см. docs/rice-scoring.md.

set -euo pipefail

PREFIX="${1:?Укажите префикс}"
TITLE="${2:?Укажите заголовок}"
LABELS="${3:-}"
BODY_FILE="${4:-}"

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ -f ".env" ]; then
    set -o allexport
    # shellcheck disable=SC1091
    source .env
    set +o allexport
fi

PROVIDER="${ISSUE_PROVIDER:-gitlab}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Получаем следующий номер.
ISSUE_ID=$("$SCRIPT_DIR/issue-counter.sh" "$PREFIX")
FULL_TITLE="$ISSUE_ID: $TITLE"

# Тело: из файла, из stdin (если pipe), или пустое.
if [ -n "$BODY_FILE" ] && [ -f "$BODY_FILE" ]; then
    BODY=$(cat "$BODY_FILE")
elif [ ! -t 0 ]; then
    BODY=$(cat)
else
    BODY="<!-- Заполните описание. Не забудьте RICE-таблицу — см. docs/rice-scoring.md. -->"
fi

echo "Создаю Issue: $FULL_TITLE"
echo "Provider: $PROVIDER"
echo "Labels:   ${LABELS:-(нет)}"

case "$PROVIDER" in
    gitlab)
        REPO="${GITLAB_GROUP:?}/${GITLAB_PROJECT:?}"
        ARGS=(issue create --repo "$REPO" --title "$FULL_TITLE" --description "$BODY")
        if [ -n "$LABELS" ]; then
            IFS=',' read -ra LBL <<< "$LABELS"
            for l in "${LBL[@]}"; do
                ARGS+=(--label "$l")
            done
        fi
        glab "${ARGS[@]}"
        ;;
    github)
        REPO="${GITHUB_REPO:?}"
        ARGS=(issue create --repo "$REPO" --title "$FULL_TITLE" --body "$BODY")
        if [ -n "$LABELS" ]; then
            ARGS+=(--label "$LABELS")
        fi
        gh "${ARGS[@]}"
        ;;
    *)
        echo "ERROR: ISSUE_PROVIDER=$PROVIDER (ожидался gitlab или github)" >&2
        exit 1
        ;;
esac
