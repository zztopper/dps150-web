#!/usr/bin/env bash
# enforce-agents-team.sh — PreToolUse hook для Task tool.
#
# Гейт:
#   1. Любой Task tool обязан быть запущен из активной Agents Team
#      (т. е. tool_input.team_name должен быть непустым).
#   2. Background агенты запрещены (tool_input.run_in_background == true → block).
#
# Контракт хука Claude Code:
#   stdin  — JSON: { "tool_name": "...", "tool_input": {...}, ... }
#   stdout — печать → передаётся обратно в transcript (информативно).
#   exit 0 — разрешить.
#   exit 2 — блокировать; stderr показывается агенту как причина блока.
#
# Скрипт намеренно тих при разрешении (никакого шума в логах).

set -euo pipefail

# Если jq недоступен — лучше fail-open с предупреждением, чем ломать сессию.
if ! command -v jq >/dev/null 2>&1; then
    echo "WARNING: jq не установлен — agents-team-hook пропущен. Установите jq." >&2
    exit 0
fi

input=$(cat)
tool_name=$(printf '%s' "$input" | jq -r '.tool_name // empty')

# Хук интересует только Task tool. Остальное пропускаем без проверок.
if [ "$tool_name" != "Task" ]; then
    exit 0
fi

team_name=$(printf '%s' "$input" | jq -r '.tool_input.team_name // empty')
run_in_background=$(printf '%s' "$input" | jq -r '.tool_input.run_in_background // false')

# Правило 2 (более строгое — проверяем первым).
if [ "$run_in_background" = "true" ]; then
    cat >&2 <<'EOF'
BLOCKED: run_in_background=true запрещено в этом проекте.

Причины:
- Background-агенты обходят координацию тимлида в Agents Team.
- Их вывод не виден в tmux-пейнах, что нарушает наблюдаемость.
- Они не подчиняются гейту обязательной DA-верификации перед коммитом.

Что делать:
1. Создать команду через TeamCreate, если её ещё нет.
2. Запустить teammate синхронно через Task с team_name (без run_in_background).
3. Несколько независимых задач — несколько Task в одном сообщении (параллельно).

Подробности: docs/agents-team-workflow.md.
EOF
    exit 2
fi

# Правило 1: Task без team_name запрещён.
if [ -z "$team_name" ] || [ "$team_name" = "null" ]; then
    cat >&2 <<'EOF'
BLOCKED: Task tool без team_name запрещён.

Причины:
- Все subagent-вызовы должны быть частью Agents Team под управлением тимлида.
- Это даёт единое окно tmux для наблюдения, общий TaskList, обмен через SendMessage.
- Гарантирует обязательный шаг DA-верификации перед коммитом.

Что делать:
1. Если команда ещё не создана — выполните TeamCreate({name: "<short-id>"}).
2. Создайте задачи через TaskCreate с зависимостями (addBlockedBy).
3. Запустите Task с параметром team_name="<тот же short-id>".
4. Несколько Task в одном сообщении = параллельный запуск teammates.

Подробности: docs/agents-team-workflow.md.
EOF
    exit 2
fi

exit 0
