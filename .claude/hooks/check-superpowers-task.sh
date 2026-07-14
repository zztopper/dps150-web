#!/usr/bin/env bash
# check-superpowers-task.sh — PreToolUse hook для Skill tool.
#
# Когда вызывается superpower-skill, способный спавнить субагентов
# (например, dispatching-parallel-agents), хук напоминает агенту правила
# оркестрации проекта. Текст зависит от режима выполнения:
#   - Agents Team (есть hooks/enforce-agents-team.sh): обернуть в TeamCreate.
#   - Ultracode Workflows (есть hooks/enforce-ultracode.sh): обернуть в Workflow.
#
# Этот хук не блокирует — даёт «soft warning» через stdout. Жёсткий гейт
# уже стоит на самом Task tool (enforce-agents-team.sh / enforce-ultracode.sh).

set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
    exit 0
fi

input=$(cat)
tool_name=$(printf '%s' "$input" | jq -r '.tool_name // empty')

if [ "$tool_name" != "Skill" ]; then
    exit 0
fi

skill=$(printf '%s' "$input" | jq -r '.tool_input.skill // empty')

hooks_dir="$(cd "$(dirname "$0")" && pwd)"
project_root="$(cd "$hooks_dir/../.." && pwd)"

# Режим выполнения: .boilerplate-mode → какой хук подключён в settings.json →
# наличие файла хука (fallback). Только presence-детекция врёт, если рядом
# лежат оба хука (репо boilerplate, проект после boilerplate-update.sh).
exec_mode=""
if [ -f "$project_root/.boilerplate-mode" ]; then
    exec_mode=$(grep '^exec=' "$project_root/.boilerplate-mode" 2>/dev/null | cut -d= -f2 || true)
fi
if [ -z "$exec_mode" ] && [ -f "$project_root/.claude/settings.json" ]; then
    if grep -q 'enforce-ultracode\.sh' "$project_root/.claude/settings.json"; then
        exec_mode=ultracode
    elif grep -q 'enforce-agents-team\.sh' "$project_root/.claude/settings.json"; then
        exec_mode=agents
    fi
fi
if [ -z "$exec_mode" ]; then
    if [ -f "$hooks_dir/enforce-ultracode.sh" ] && [ ! -f "$hooks_dir/enforce-agents-team.sh" ]; then
        exec_mode=ultracode
    else
        exec_mode=agents
    fi
fi

case "$skill" in
    dispatching-parallel-agents|*parallel-agents*|*spawn-agents*|*subagent-driven*)
        if [ "$exec_mode" = "ultracode" ]; then
            cat <<'EOF'
NOTE: superpower-skill, который обычно спавнит субагентов через Task.
В этом проекте действует политика Ultracode Workflows:
  - Параллельный спавн агентов — только внутри Workflow tool
    (фазы, agent()/parallel()/pipeline()).
  - Task с team_name запрещён (хук enforce-ultracode.sh блокирует).
  - run_in_background=true запрещено.
Перед запуском убедитесь, что:
  1. Работа оформлена как Workflow-скрипт с фазами.
  2. Финальная фаза — adversarial verify (Devil's Advocate).
Подробности: docs/ultracode-workflow.md.
EOF
        else
            cat <<'EOF'
NOTE: superpower-skill, который обычно спавнит субагентов через Task.
В этом проекте действует политика:
  - Любой Task должен иметь team_name (хук enforce-agents-team.sh блокирует иное).
  - run_in_background=true запрещено.
Перед запуском убедитесь, что:
  1. Создана команда: TeamCreate({name: "<short-id>"}).
  2. Все Task передают этот team_name.
  3. Никакого run_in_background.
Подробности: docs/superpowers-integration.md.
EOF
        fi
        ;;
esac

exit 0
