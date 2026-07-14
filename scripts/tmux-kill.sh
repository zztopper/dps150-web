#!/usr/bin/env bash
# tmux-kill.sh — закрыть tmux-сессию Agents Team.
#
# Использование:
#   ./scripts/tmux-kill.sh                  # закрыть сессию claude-team
#   ./scripts/tmux-kill.sh my-feature       # закрыть сессию my-feature

set -euo pipefail

SESSION="${1:-claude-team}"

if ! tmux has-session -t "$SESSION" 2>/dev/null; then
    echo "Сессия '$SESSION' не найдена."
    exit 0
fi

tmux kill-session -t "$SESSION"
echo "Сессия '$SESSION' закрыта."
