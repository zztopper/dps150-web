#!/usr/bin/env bash
# adr-create.sh — работа с ADR в Wiki проекта.
#
# Режимы:
#   --next-number              Печатает следующий свободный номер ADR.
#   <title>                    Печатает шаблон ADR-NNN с подставленным title в stdout.
#   --publish <title> <file>   Публикует <file> как страницу ADR в Wiki проекта.
#
# Поддерживает GitLab (через glab api) и GitHub (через клон wiki-репозитория).
# ISSUE_PROVIDER в .env определяет провайдера.

set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ -f ".env" ]; then
    set -o allexport
    # shellcheck disable=SC1091
    source .env
    set +o allexport
fi

PROVIDER="${ISSUE_PROVIDER:-gitlab}"

# --- helper: получить следующий номер ADR ---
adr_next_number() {
    local titles
    case "$PROVIDER" in
        gitlab)
            local enc="${GITLAB_GROUP:?}%2F${GITLAB_PROJECT:?}"
            titles=$(glab api "projects/$enc/wikis" 2>/dev/null \
                     | jq -r '.[].title' || true)
            ;;
        github)
            # Clone wiki repo to a temp dir и считываем имена .md файлов.
            local tmp
            tmp=$(mktemp -d)
            local wiki_url="https://github.com/${GITHUB_REPO:?}.wiki.git"
            git clone --depth 1 --quiet "$wiki_url" "$tmp" 2>/dev/null || mkdir -p "$tmp"
            titles=$(find "$tmp" -maxdepth 1 -name '*.md' -exec basename {} \; 2>/dev/null || true)
            rm -rf "$tmp"
            ;;
        *)
            echo "ERROR: ISSUE_PROVIDER=$PROVIDER" >&2; exit 1 ;;
    esac

    printf '%s\n' "$titles" | awk '
        match($0, /ADR-[0-9]+/) {
            n = substr($0, RSTART + 4, RLENGTH - 4)
            if (n + 0 > max) max = n + 0
        }
        END { printf "%03d\n", (max + 1) }
    '
}

# --- helper: шаблон ADR ---
adr_template() {
    local num="$1" title="$2"
    cat <<EOF
# ADR-$num: $title

## Status
Proposed

Date: $(date +%F)
Authors: <agent-role>
Reviewers: <reviewer>

## Context
<!-- Описание проблемы, ограничений, мотивации. -->

## Decision
<!-- Что выбрали и как реализуем на верхнем уровне. -->

## Consequences

### Положительные
-

### Отрицательные
-

### Риски и митигация
-

## Alternatives Considered

| Вариант | Плюсы | Минусы | Почему отклонён |
|---------|-------|--------|------------------|

## Links
- Issues:
- MR/PR:
- Связанные ADR:
EOF
}

# --- helper: публикация в Wiki ---
adr_publish_gitlab() {
    local title="$1" content_file="$2"
    local enc="${GITLAB_GROUP:?}%2F${GITLAB_PROJECT:?}"
    local content
    content=$(cat "$content_file")

    glab api "projects/$enc/wikis" \
        -X POST \
        -F "title=$title" \
        -F "content=$content" \
        -F "format=markdown"
}

adr_publish_github() {
    local title="$1" content_file="$2"
    local tmp
    tmp=$(mktemp -d)
    local wiki_url="https://github.com/${GITHUB_REPO:?}.wiki.git"

    if ! git clone --quiet "$wiki_url" "$tmp" 2>/dev/null; then
        echo "WARN: wiki репозиторий не существует. Создайте первую страницу через UI." >&2
        rm -rf "$tmp"
        return 1
    fi

    # Имя файла = title с дефисами, .md
    local filename
    filename=$(printf '%s' "$title" | tr ' /' '--').md
    cp "$content_file" "$tmp/$filename"
    (
        cd "$tmp"
        git add "$filename"
        git -c user.email="claude-code@boilerplate" -c user.name="claude-code" \
            commit -m "Add $title"
        git push --quiet origin master 2>/dev/null \
            || git push --quiet origin main
    )
    rm -rf "$tmp"
    echo "Опубликовано: $title (https://github.com/${GITHUB_REPO}/wiki/$filename)"
}

# ====== main ======

case "${1:-}" in
    --next-number)
        adr_next_number
        ;;

    --publish)
        TITLE="${2:?Укажите title}"
        FILE="${3:?Укажите файл с содержимым}"
        if [ ! -f "$FILE" ]; then
            echo "ERROR: $FILE не найден" >&2; exit 1
        fi
        case "$PROVIDER" in
            gitlab) adr_publish_gitlab "$TITLE" "$FILE" ;;
            github) adr_publish_github "$TITLE" "$FILE" ;;
            *)      echo "ERROR: ISSUE_PROVIDER=$PROVIDER" >&2; exit 1 ;;
        esac
        ;;

    "")
        echo "Использование:"
        echo "  $0 --next-number"
        echo "  $0 \"<title>\""
        echo "  $0 --publish \"ADR-NNN: <title>\" <file.md>"
        exit 1
        ;;

    *)
        TITLE="$1"
        NUM=$(adr_next_number)
        adr_template "$NUM" "$TITLE"
        ;;
esac
