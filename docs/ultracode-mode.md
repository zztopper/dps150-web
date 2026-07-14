# Ultracode-режим бойлерплейта

Ultracode — режим **выполнения задач**: оркестрация субагентов через
встроенный **Workflow tool** Claude Code (фазы, `agent()`/`parallel()`/
`pipeline()`, adversarial verify) вместо Agents Team
(TeamCreate + tmux-пейны + team_name-гейт).

## Две независимые оси конфигурации

`init-project.sh` принимает флаги двух независимых осей:

| Ось                  | Значения                          | За что отвечает                                          |
|----------------------|-----------------------------------|----------------------------------------------------------|
| **Профиль**          | full (дефолт) / `--lite`          | Состав процессов и доков, пороги качества, организация трекинга задач (Issues в GitLab/GitHub) |
| **Режим выполнения** | `--agents` (дефолт) / `--ultracode` | Механика оркестрации субагентов                          |

Любые комбинации валидны: `--ultracode`, `--lite --ultracode`,
`--lite --agents` и т.д. `--agents --ultracode` одновременно — ошибка.

## Запуск

```bash
# enterprise-профиль + ultracode-оркестрация
./init-project.sh --ultracode /path/to/project my-app "Описание" gitlab.example.com grp

# lite-профиль + ultracode-оркестрация
./init-project.sh --lite --ultracode /path/to/project my-app "Описание" gitlab.example.com grp
```

Без `--ultracode` разворачивается Agents Team (поведение по умолчанию
не меняется; `--agents` — явный синоним дефолта).

## Что МЕНЯЕТСЯ в ultracode

Не копируются (привязаны к механике Agents Team):
- `scripts/tmux-team.sh`, `scripts/tmux-kill.sh` (tmux-пейны не нужны —
  прогресс наблюдается через `/workflows`)
- `scripts/agents-cost-report.sh` (парсит TaskList/TaskGet команды)
- `.claude/hooks/enforce-agents-team.sh` (блокировал бы любой subagent
  без team_name — несовместим с Workflow-оркестрацией)
- `docs/agents-team-workflow.md`

Добавляются вместо них:
- `.claude/hooks/enforce-ultracode.sh` — PreToolUse-гейт: блокирует
  `Task` с `team_name` и `run_in_background=true`; одиночный синхронный
  `Task` разрешён. Жёсткий гейт сознательно стоит только на `Task`
  (Workflow tool и Team-инструменты не матчатся PreToolUse-хуком на Task);
  правило «substantive-задачи — через Workflow» — процессное, из CLAUDE.md.
- `docs/ultracode-workflow.md` — правила оркестратора: фазы, параллелизм,
  обязательная adversarial-verify-фаза (Devil's Advocate), лимит 3 циклов,
  human checkpoints.

Правятся при init:
- `.claude/settings.json` — hook-гейт на `Task` переключается на
  `enforce-ultracode.sh`.
- `.claude/settings.local.json` — tmux-разрешения удаляются (если есть `jq`).
- `*.md` — срезаются `agents-team-only`-блоки, остаются `ultracode-only`.

## Что НЕ меняется

- **Роли агентов** (`.claude/skills/*.md`) — те же: Architect, SDE,
  Devil's Advocate и т.д. Меняется только механика спавна: контекст роли
  передаётся в `agent()` внутри workflow.
- **Гейт Devil's Advocate** — обязателен в обоих режимах; в ultracode это
  финальная adversarial-verify-фаза workflow.
- **Правило «находка без следа запрещена»** — каждая неустранённая находка
  DA оформляется как Issue.
- **Трекинг задач** — Issues в GitLab/GitHub (ось выполнения на него
  не влияет).
- RICE-оценка тремя голосами, ADR-workflow, DoR/DoD, git-workflow,
  token-budget (бюджеты в терминах агентов/фаз).

## Как механически работает ultracode

Та же механика, что у `--lite` (см. `docs/lite-mode.md`), вторая пара маркеров:
1. **Список-исключение** `EXEC_EXCLUDE` в `init-project.sh` — какие файлы
   не копируются в каждом режиме выполнения (в agents-режиме исключаются
   ultracode-артефакты, и наоборот).
2. **Маркеры секций** в копируемых `*.md` — HTML-комментарии
   `agents-team-only:start/end` и `ultracode-only:start/end`.
   При `--ultracode` init вырезает `agents-team-only`-блоки и оставляет
   `ultracode-only`; в agents-режиме — наоборот.
3. **Подмена hook-гейта** в `.claude/settings.json` (sed) — структура
   конфига одинакова, меняется только путь скрипта.

Маркеры двух осей (профиль и exec) **нельзя вкладывать друг в друга** —
только соседние блоки.

Выбранные оси персистятся в `.boilerplate-mode` (`profile=...`, `exec=...`).

## Ограничение boilerplate-update.sh

`scripts/boilerplate-update.sh --apply` копирует upstream-файлы целиком —
он не пересоздаёт исключения и не срезает маркеры (это касается и `--lite`).
В ultracode-проекте после `--apply` проверьте и удалите реанимированные
agents-team-артефакты (список выше) и верните `enforce-ultracode.sh`
в `.claude/settings.json`. Скрипт предупреждает об этом, читая
`.boilerplate-mode`.

## Как переключить режим выполнения позже

Перезапустите `init-project.sh` с нужным флагом поверх проекта. Артефакты
прежнего режима удаляются автоматически (`EXEC_EXCLUDE`), `settings.json`
переключается сам. В backup `.boilerplate-backup/<timestamp>/` попадают
только файлы, управляемые boilerplate (`.claude/`, `.serena/`, `scripts/`,
`docs/`, `templates/` и корневые `CLAUDE.md`, `.mcp.json` и т.п.) —
плейсхолдеры и маркеры в остальных файлах проекта init не трогает.
Сверьте после переключения только собственные правки в managed-путях,
ушедшие в backup.
