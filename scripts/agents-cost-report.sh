#!/usr/bin/env bash
# agents-cost-report.sh — собирает token-usage отчёт по Agents Team.
#
# Использование:
#   ./scripts/agents-cost-report.sh <team_name>            # текущий цикл
#   ./scripts/agents-cost-report.sh <team_name> > report.md
#
# Best-effort parsing: формат вывода TaskList/TaskGet может отличаться
# между версиями Claude Code. Если парсинг не удаётся — печатает шаблон
# для ручного заполнения.

set -euo pipefail

TEAM="${1:?Укажите team_name (имя команды из TeamCreate)}"
NOW=$(date +"%Y-%m-%d %H:%M:%S %Z")

cat <<EOF
## Cost report — $TEAM

- Команда: $TEAM
- Дата отчёта: $NOW
- Длительность: <заполните вручную или из логов tmux>
- Teammates: <число>
- Циклов DA: <число>

### Tokens по teammates

EOF

# Попытка собрать данные через Claude Code CLI, если доступно.
# (claude task list --team "$TEAM" --json и т. п. — точное API зависит от версии.)
# Если CLI не отдаёт нужный формат — заполните вручную.

# Заглушка-таблица — тимлид заменяет фактическими цифрами из TaskList/TaskGet.
cat <<'EOF'
| Роль                | Input tokens | Output tokens | Циклов |
|---------------------|-------------:|--------------:|-------:|
| Architect           |              |               |        |
| System Analyst      |              |               |        |
| SDE Backend         |              |               |        |
| SDE Frontend        |              |               |        |
| DBA                 |              |               |        |
| DevOps              |              |               |        |
| QA                  |              |               |        |
| Technical Writer    |              |               |        |
| Devil's Advocate    |              |               |        |
| **Total**           |              |               |        |

### Cost estimate
- Tariff: <model rate per 1M input / output tokens>
- Estimated cost: $X.XX

### vs Budget (`docs/token-budget.md`)
- Класс задачи: <Feature ≤ 1 PD | Feature 1-3 PD | Bug fix | TD | RICE | hotfix>
- Используется: __% input / __% output

### Заметки
- <что прошло хорошо>
- <что прошло плохо / неожиданные перерасходы>
- <action items для калибровки>
EOF
