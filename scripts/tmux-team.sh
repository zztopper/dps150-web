#!/usr/bin/env bash
# tmux-team.sh — открыть tmux-сессию для работы с Agents Team
#
# Создаёт tmux-окно с разбиением на пейны:
#   ┌────────────────────┬────────────────────┐
#   │  pane-0: claude    │  pane-2: agent-2   │
#   │  (главный)         │                    │
#   ├────────────────────┼────────────────────┤
#   │  pane-1: agent-1   │  pane-3: agent-3   │
#   └────────────────────┴────────────────────┘
#
# Главный pane запускает основного клиента Claude Code, который через
# TeamCreate + Task spawn-ит teammates. Каждый teammate автоматически
# получит свой pane (Claude Code разделит окно сам), либо вручную
# присоединяйтесь к ним по именам через `tmux select-pane`.
#
# Использование:
#   ./scripts/tmux-team.sh                    # сессия с именем по дефолту
#   ./scripts/tmux-team.sh my-feature         # сессия my-feature
#   ./scripts/tmux-team.sh my-feature 4       # 4 пейна (1 главный + 3 для teammates)
#
# После запуска:
#   - Внутри pane-0 работаете с лидером (Claude Code).
#   - При TeamCreate + Task с team_name — Claude Code сам распределит teammates по pane-ам.
#   - Чтобы переключаться: Ctrl-b затем стрелки или o.
#   - Чтобы выйти из tmux, не убивая сессию: Ctrl-b затем d.
#   - Чтобы вернуться: tmux attach -t <session_name>.

set -euo pipefail

SESSION="${1:-claude-team}"
PANES="${2:-4}"

if ! command -v tmux >/dev/null 2>&1; then
    echo "ERROR: tmux не установлен. macOS: brew install tmux. Linux: apt/dnf install tmux." >&2
    exit 1
fi

if ! command -v claude >/dev/null 2>&1; then
    echo "WARNING: команда 'claude' не найдена в PATH. Проверьте установку Claude Code." >&2
fi

if tmux has-session -t "$SESSION" 2>/dev/null; then
    echo "Сессия '$SESSION' уже существует. Подключаюсь..."
    exec tmux attach -t "$SESSION"
fi

PROJECT_DIR="$(pwd)"

# Создаём сессию с главным pane (pane-0).
tmux new-session -d -s "$SESSION" -c "$PROJECT_DIR" -n "team"

# Делим окно на сетку для teammates. Claude Code сам пересоберёт layout под teammates;
# здесь мы просто гарантируем, что окно достаточно большое и есть точки подключения.
case "$PANES" in
    2)
        tmux split-window -h -t "$SESSION:team" -c "$PROJECT_DIR"
        ;;
    3)
        tmux split-window -h -t "$SESSION:team" -c "$PROJECT_DIR"
        tmux split-window -v -t "$SESSION:team.1" -c "$PROJECT_DIR"
        ;;
    4|*)
        tmux split-window -h -t "$SESSION:team" -c "$PROJECT_DIR"
        tmux split-window -v -t "$SESSION:team.0" -c "$PROJECT_DIR"
        tmux split-window -v -t "$SESSION:team.2" -c "$PROJECT_DIR"
        ;;
esac

tmux select-layout -t "$SESSION:team" tiled
tmux select-pane -t "$SESSION:team.0"

# Запускаем claude в главном pane.
if command -v claude >/dev/null 2>&1; then
    tmux send-keys -t "$SESSION:team.0" "claude" Enter
else
    tmux send-keys -t "$SESSION:team.0" "echo 'Установите Claude Code и запустите: claude'" Enter
fi

# Подсказки для остальных пейнов.
PANE_COUNT=$(tmux list-panes -t "$SESSION:team" | wc -l | tr -d ' ')
for i in $(seq 1 $((PANE_COUNT - 1))); do
    tmux send-keys -t "$SESSION:team.$i" "echo 'Pane $i — для teammate-агента (TeamCreate + Task spawn заполнит автоматически).'" Enter
done

echo "Сессия '$SESSION' создана с $PANE_COUNT пейнами в $PROJECT_DIR."
echo "Подключаюсь..."
exec tmux attach -t "$SESSION"
