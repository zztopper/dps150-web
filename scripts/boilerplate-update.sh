#!/usr/bin/env bash
# boilerplate-update.sh — pull обновлений из upstream boilerplate в текущий проект.
#
# Логика:
#   1. Прочитать .boilerplate-version в текущем проекте.
#   2. Прочитать VERSION в upstream-репо boilerplate (BOILERPLATE_PATH или клонировать).
#   3. Если версии равны — выйти, обновлений нет.
#   4. Иначе — скопировать каждый файл boilerplate в .boilerplate-update/<timestamp>/
#      и предложить ручной merge с показом diff.
#   5. После одобрения пользователем — применить изменения.
#
# По умолчанию НЕ перетирает файлы. Все изменения требуют ручного approve.
#
# Использование:
#   ./scripts/boilerplate-update.sh
#   BOILERPLATE_PATH=/path/to/boilerplate ./scripts/boilerplate-update.sh
#   ./scripts/boilerplate-update.sh --dry-run     # только показать diff
#   ./scripts/boilerplate-update.sh --apply       # применить без интерактива (после dry-run)

set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

MODE="interactive"
case "${1:-}" in
    --dry-run) MODE="dry-run" ;;
    --apply)   MODE="apply"   ;;
    "") ;;
    *) echo "Неизвестный режим: $1"; exit 2 ;;
esac

# 1. Текущая версия в проекте.
if [ ! -f ".boilerplate-version" ]; then
    echo "ERROR: .boilerplate-version не найден. Похоже, проект инициализирован" >&2
    echo "       старой версией boilerplate. Создайте файл вручную с текущей" >&2
    echo "       версией (например, '1.0.0') и повторите." >&2
    exit 1
fi
LOCAL_VERSION=$(cat .boilerplate-version)

# 2. Upstream-версия.
UPSTREAM="${BOILERPLATE_PATH:-$HOME/Sources/claude-project-boilerplate}"
if [ ! -d "$UPSTREAM" ]; then
    echo "ERROR: upstream boilerplate не найден в $UPSTREAM" >&2
    echo "       Установите BOILERPLATE_PATH=/path/to/boilerplate" >&2
    exit 1
fi
UPSTREAM_VERSION=$(cat "$UPSTREAM/VERSION")

echo "Local:    $LOCAL_VERSION"
echo "Upstream: $UPSTREAM_VERSION"

if [ "$LOCAL_VERSION" = "$UPSTREAM_VERSION" ]; then
    echo "Версии совпадают, обновлений нет."
    exit 0
fi

echo ""
echo "=== Diff между local и upstream ==="
echo ""

# Определяем, какие файлы изменились в upstream.
TMP_LIST=$(mktemp)
trap 'rm -f "$TMP_LIST"' EXIT

# Сравниваем по тем же путям, что копирует init-project.sh.
PATHS=(.claude .serena scripts docs templates CLAUDE.md .mcp.json .env.example .gitignore VERSION CHANGELOG.md)

for p in "${PATHS[@]}"; do
    if [ ! -e "$UPSTREAM/$p" ]; then
        continue
    fi
    if [ ! -e "$p" ]; then
        echo "+ NEW:     $p"
        echo "$p:NEW" >> "$TMP_LIST"
        continue
    fi
    # diff -r returns 1 if differs, 0 if same.
    if ! diff -rq "$p" "$UPSTREAM/$p" > /dev/null 2>&1; then
        echo "~ CHANGED: $p"
        echo "$p:CHANGED" >> "$TMP_LIST"
    fi
done

if [ ! -s "$TMP_LIST" ]; then
    echo "Файлы идентичны (хотя версии разные?). Возможно, обновлена только VERSION/CHANGELOG."
    echo "Обновить локальный .boilerplate-version до $UPSTREAM_VERSION? [y/N]"
    read -r ans
    if [ "$ans" = "y" ] || [ "$ans" = "Y" ]; then
        echo "$UPSTREAM_VERSION" > .boilerplate-version
        echo "Готово."
    fi
    exit 0
fi

# Режим проекта (для предупреждения и post-apply чистки).
B_PROFILE=full
B_EXEC=agents
if [ -f ".boilerplate-mode" ]; then
    B_PROFILE=$(grep '^profile=' .boilerplate-mode | cut -d= -f2 || echo full)
    B_EXEC=$(grep '^exec=' .boilerplate-mode | cut -d= -f2 || echo agents)
fi

# apply копирует upstream-файлы КАК ЕСТЬ: с сырыми {{плейсхолдерами}} и
# несрезанными маркерами *-only. Это касается ЛЮБОГО проекта.
echo ""
echo "⚠️  apply копирует upstream-файлы как есть: в изменённых файлах вернутся"
echo "   сырые {{плейсхолдеры}} и маркеры *-only (enterprise/lite/agents-team/"
echo "   ultracode) — их придётся поправить вручную (сверьтесь с diff)."
echo "   Файлы, исключаемые init-project.sh для profile=$B_PROFILE, exec=$B_EXEC,"
echo "   будут удалены автоматически после копирования."
echo ""

if [ "$MODE" = "dry-run" ]; then
    echo "Dry-run: изменений не применено. Запустите с --apply для применения."
    exit 0
fi

# 3. Backup и применение.
BACKUP_DIR=".boilerplate-update/$(date +%Y%m%d-%H%M%S)"
echo ""
echo "Будет создан backup в: $BACKUP_DIR"
mkdir -p "$BACKUP_DIR"

if [ "$MODE" = "interactive" ]; then
    echo "Применить изменения? [y/N]"
    read -r ans
    if [ "$ans" != "y" ] && [ "$ans" != "Y" ]; then
        echo "Отменено."
        rm -rf "$BACKUP_DIR"
        exit 0
    fi
fi

while IFS=':' read -r p status; do
    if [ "$status" = "CHANGED" ] && [ -e "$p" ]; then
        rel_dir=$(dirname "$p")
        mkdir -p "$BACKUP_DIR/$rel_dir"
        cp -R "$p" "$BACKUP_DIR/$rel_dir/" 2>/dev/null || true
    fi
    cp -R "$UPSTREAM/$p" "$(dirname "$p")/"
    echo "  applied: $p"
done < "$TMP_LIST"

# 4. Post-apply чистка по осям проекта: повторяем исключения init-project.sh
#    (списки держать в синхроне с init-project.sh: LITE_EXCLUDE / EXEC_EXCLUDE).
rm -rf docs/superpowers   # внутренние артефакты boilerplate, не для проектов

if [ "$B_PROFILE" = "lite" ]; then
    rm -f docs/security-policy.md docs/observability.md docs/incident-response.md \
          docs/feature-flags.md docs/performance-budgets.md docs/accessibility-i18n.md \
          docs/api-versioning.md .claude/skills/sre-engineer.md
fi

if [ "$B_EXEC" = "ultracode" ]; then
    rm -f scripts/tmux-team.sh scripts/tmux-kill.sh scripts/agents-cost-report.sh \
          .claude/hooks/enforce-agents-team.sh docs/agents-team-workflow.md
    # Свежескопированный settings.json снова указывает на agents-хук — вернуть.
    if [ -f ".claude/settings.json" ]; then
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' 's|enforce-agents-team\.sh|enforce-ultracode.sh|g' .claude/settings.json
        else
            sed -i 's|enforce-agents-team\.sh|enforce-ultracode.sh|g' .claude/settings.json
        fi
    fi
else
    rm -f .claude/hooks/enforce-ultracode.sh docs/ultracode-workflow.md
fi

# 5. Обновляем .boilerplate-version.
echo "$UPSTREAM_VERSION" > .boilerplate-version

echo ""
echo "Обновление применено. Backup: $BACKUP_DIR"
echo "Не забудьте:"
echo "  - Проверить diff и сделать commit (Conventional Commits: chore: bump boilerplate to $UPSTREAM_VERSION)"
echo "  - Перечитать CHANGELOG.md (upstream) на breaking changes."
