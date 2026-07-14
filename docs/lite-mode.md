# Lite-режим бойлерплейта

Lite — облегчённый профиль для **пет-проектов, домашних и с минимальной
финансовой нагрузкой**. Сохраняет полный рабочий флоу (GitLab Issues → MR →
деплой → код-ревью → двойной контроль через Devil's Advocate), но убирает
тяжёлые ops/compliance-практики, которые оправданы только при реальной нагрузке
и деньгах.

Lite — ось **профиля** проекта. Ось **режима выполнения**
(`--agents`, по умолчанию / `--ultracode`) от неё независима и свободно
комбинируется (`--lite --ultracode`) — см. `docs/ultracode-mode.md`.

## Запуск

```bash
./init-project.sh --lite /path/to/project my-app "Описание" gitlab.example.com grp
```

Без `--lite` разворачивается полный (enterprise) набор — поведение по умолчанию
не меняется.

## Что НЕ входит в lite

Удаляются документы:
- `docs/security-policy.md` (SAST/SCA/SBOM/threat modelling)
- `docs/observability.md` (SLO/метрики/трейсы/runbooks)
- `docs/incident-response.md` (severity/IC/post-mortem)
- `docs/feature-flags.md` (feature flags / kill switch)
- `docs/performance-budgets.md` (p50/p95/p99, Core Web Vitals)
- `docs/accessibility-i18n.md` (WCAG 2.1 AA + i18n)
- `docs/api-versioning.md` (SemVer внешнего API + deprecation)

Удаляется агент:
- `.claude/skills/sre-engineer.md` (его базовый K8s-troubleshooting переходит к
  DevOps-агенту).

Смягчаются пороги:
- Покрытие тестами: ориентир ~60 % на core/handlers вместо ≥ 70/85 %.

## Что ОСТАЁТСЯ в lite (полный флоу)

- Agents Team в tmux + hooks-гейты (`enforce-agents-team.sh`).
- GitLab Issues с префиксами и сквозной нумерацией; RICE-оценка тремя голосами.
- MR-флоу: `docs/git-workflow.md` (almost-TBD, branch protection,
  Conventional Commits, деплой из тегов).
- `docs/release-process.md`, `docs/migration-safety.md`.
- Двойной контроль до прода: код-ревью + **обязательный Devil's Advocate**,
  `docs/definition-of-ready.md`, `docs/definition-of-done.md`,
  `docs/pre-commit-gate.md`, `docs/testing-pyramid.md`.
- ADR (`docs/adr-workflow.md`) и LikeC4 (`docs/likec4-workflow.md` + `.c4`).
- Контроль стоимости: `docs/token-budget.md`, `scripts/agents-cost-report.sh`.
- `docs/da-self-review.md` (пример DA-отчёта), `docs/mcp-servers.md`.
- Все скрипты выбранного режима выполнения и все MCP-серверы (можно удалить
  лишние вручную — см. SETUP.md §10b).

## Как механически работает lite

Один источник истины — файлы не дублируются:
1. **Список-исключение** в `init-project.sh` — какие файлы не копируются при
   `--lite`.
2. **Маркеры секций** в копируемых `*.md` — HTML-комментарии вида
   `enterprise-only:start` / `enterprise-only:end` и
   `lite-only:start` / `lite-only:end`. При `--lite` init вырезает
   `enterprise-only`-блоки и оставляет `lite-only`; без флага — наоборот.

Пар маркеров две, и они отвечают за независимые оси:
- `enterprise-only` / `lite-only` — ось **профиля** (`--lite`);
- `agents-team-only` / `ultracode-only` — ось **режима выполнения**
  (`--agents` / `--ultracode`, см. `docs/ultracode-mode.md`).

Пары разных осей **не вкладываются** друг в друга — только соседние блоки.

Чтобы вернуть отдельный enterprise-документ в lite-проект — скопируйте его из
бойлерплейта вручную:
```bash
cp /path/to/boilerplate/docs/observability.md docs/
```

## Когда переходить с lite на enterprise

Если проект начал нести реальную нагрузку/деньги — доустановите нужные доки из
полного набора (`init-project.sh` без `--lite` в отдельную папку для сравнения,
либо точечный `cp`). Lite намеренно не запрещает рост.
