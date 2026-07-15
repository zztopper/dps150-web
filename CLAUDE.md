# dps150-web — Web-based control panel for FNIRSI DPS-150 programmable power supply

{{Краткое описание проекта — 1-2 предложения.}}

## Обязательное использование ARCHITECTURE.md

**ВАЖНО:** Перед любой работой, затрагивающей архитектуру, API, сущности, схему БД, инфраструктуру или деплой, **обязательно** прочитать `docs/architecture/ARCHITECTURE.md` (стек, сущности, RBAC, endpoints, K8s, CI/CD). Не дублировать его содержимое здесь.

## Команды разработки

```bash
# Локальная разработка
make dev              # Запустить зависимости (DB, кеш, хранилище)
make migrate          # Применить миграции
make dev-backend      # Запустить backend
make dev-frontend     # Запустить frontend dev server

# Тестирование
make test             # Unit-тесты
make test-cover       # С покрытием
make test-int         # Integration-тесты

# Сборка
make build            # Собрать бинарники
make build-docker     # Собрать Docker-образы

# Линтинг
make lint             # Backend + Frontend linters

# Миграции
./bin/migrate up              # Применить все миграции
./bin/migrate down 1          # Откатить 1 миграцию
./bin/migrate version         # Текущая версия
```

## CLI Tools

| Инструмент | Назначение | Адрес/Конфиг |
|------------|------------|--------------|
| `glab` | GitLab CLI (если ISSUE_PROVIDER=gitlab) | git.example.com |
| `gh` | GitHub CLI (если ISSUE_PROVIDER=github) | github.com |
| `tmux` | Окно с пейнами для Agents Team | — |
| `vault` | HashiCorp Vault CLI (опционально) | — |
| `kubectl` | Kubernetes CLI | ~/.kube/config |
| `helm` | Helm charts | — |

## Hooks (PreToolUse — гейт на subagents)

В `.claude/settings.json` настроены **обязательные** hooks:

- **`Task` без `team_name`** → блокируется (`enforce-agents-team.sh`).
  Все subagent-вызовы — только в рамках Agents Team под управлением тимлида.
- **`Task` c `run_in_background=true`** → блокируется. Background-агенты запрещены.
- **`Skill`** с superpower-именами, спавнящими агентов → soft-warning с
  напоминанием обернуть в TeamCreate.

Hooks применяются к каждому запуску `claude` в репо. Не пытайтесь их обойти —
любая такая попытка означает нарушение процесса (см. `docs/superpowers-integration.md`).

## Helper-скрипты

Скрипты в `./scripts/` обёртывают рутинные операции (агентам они доступны через Bash):

| Скрипт                              | Назначение                                                |
|-------------------------------------|------------------------------------------------------------|
| `scripts/tmux-team.sh [name] [N]`   | Открыть tmux-сессию для Agents Team (N пейнов)             |
| `scripts/tmux-kill.sh [name]`       | Закрыть tmux-сессию                                        |
| `scripts/setup-secrets.sh`          | Авторизовать `glab`/`gh` из `.env`                         |
| `scripts/issue-counter.sh PREFIX`   | Получить следующий номер Issue для префикса (F/B/TD/…)     |
| `scripts/issue-create.sh ...`       | Создать Issue с правильным заголовком и labels             |
| `scripts/issue-list.sh ...`         | Список Issues по статусу/labels                            |
| `scripts/rice-template.sh [t]`      | Шаблон описания Issue с RICE-таблицей                       |
| `scripts/adr-create.sh [args]`      | Шаблон ADR / публикация ADR в Wiki проекта                  |
| `scripts/precommit-check.sh`        | Pre-commit gate: build + lint + tests + likec4 validate     |
| `scripts/agents-cost-report.sh`     | Cost report по Agents Team после `TeamDelete`                |
| `scripts/boilerplate-update.sh`     | Pull обновлений из upstream boilerplate с интерактивным merge |

## Правила для AI-агентов

- **Модель** — выбирать строго по характеру задачи (не «по умолчанию»):
  - **opus** — default. Любая минимальная сложность: реализация, рефакторинг, ревью,
    архитектура, спецификации, отладка, оценка, тесты с логикой. Все ключевые роли
    запускаются на opus.
  - **haiku** — исследования без принятия решений: чтение/поиск кода, сбор фактов,
    инвентаризация, навигация по большому объёму, Explore-сценарии.
  - **sonnet** — только тривиальные исполнительские задачи: переименование,
    форматирование, генерация повторяющихся файлов по жёсткому шаблону.
  - Если есть выбор/оценка — это уже **opus**.
  - Всегда указывать `model` при запуске субагентов через `Task` tool.
- **JSON-парсинг**: использовать `jq`, не `python3`
- **Issue-tracker**: `ISSUE_PROVIDER` в `.env` — `gitlab` или `github`. Используйте `glab` или `gh` соответственно. URL-encoded project path для GitLab API: `applications%2Fdps150-web`
- **Context7**: Always use Context7 MCP when I need library/API documentation, code generation, setup or configuration steps without me having to explicitly ask.
- **Extended thinking**: При нетривиальных решениях (architecture, ambiguous AC, конфликт ограничений) использовать встроенное extended thinking Claude 4 — не MCP-сервер. (sequential-thinking MCP исключён из набора по соображениям безопасности.)
- **Serena**: Always use Serena MCP for semantic search and actions over the codebase.

## Переменные окружения

```env
SERVER_PORT=8080
DATABASE_HOST=localhost
DATABASE_PORT=5432
DATABASE_USER=dps150-web
DATABASE_PASSWORD=secret
DATABASE_NAME=dps150-web
REDIS_HOST=localhost
REDIS_PORT=6379
JWT_SECRET=your-secret-key
JWT_ACCESS_TOKEN_TTL=15m
JWT_REFRESH_TOKEN_TTL=168h
LOG_LEVEL=debug
LOG_FORMAT=console
```

## Специализированные агенты

Использовать Agents Team (TeamCreate + SendMessage). Контекст агентов в `.claude/skills/`.
Все агенты должны использовать встроенное extended thinking Claude 4 при нетривиальных решениях.
SDE-агенты, а также Architect и System Analyst должны использовать Serena MCP (serena) для семантического поиска.
Для поиска документации/справочной информации — обращаться к MCP Context7.

Workflow: `TeamCreate` -> `TaskCreate` (с зависимостями) -> `Task` (spawn teammates с `team_name`) -> `SendMessage` -> `TaskList` -> `TeamDelete`

### Правила тимлида (главный Claude в pane-0)

**Полный документ — `docs/agents-team-workflow.md`. Краткая выжимка:**

1. **Максимальный параллелизм по умолчанию.** Скорость и качество **приоритетнее
   экономии токенов**. Запускай teammates **одним сообщением** с несколькими
   `Task`-вызовами; зависимости — через `addBlockedBy`, а не последовательность.
2. **Состав ролей — под задачу.** Бери только нужных реализаторов
   (Backend / Frontend / DBA / DevOps / QA / Tech Writer и т. д.).
3. **Все ключевые роли — model: opus** (default). См. таблицу выбора модели —
   sonnet/haiku берутся только под исследования или тривиальные исполнительские
   задачи (см. правило в начале CLAUDE.md и `docs/agents-team-workflow.md` § 3).
4. **ОБЯЗАТЕЛЬНАЯ верификация Devil's Advocate перед коммитом.** В каждом
   реализационном цикле финальный шаг — `Task(devils-advocate, model: opus)`.
   Без зелёного отчёта DA коммит/мердж запрещён.
5. **Все находки DA — устранить или зафиксировать как Issue.**
   - CRITICAL/HIGH — устранить **в текущей сессии**, без вариантов.
   - **Каждая** оставшаяся находка (MEDIUM/LOW/WISHES, pre-existing,
     out-of-scope, любой иной случай неустранения в сессии) — оформляется
     как **новый Issue** в трекере (`./scripts/issue-create.sh`).
     Без исключений. «Находка без следа» запрещена.
6. **Цикл «найди → исправь → перепроверь»** — повторять до зелёного DA-отчёта.

### Инфраструктура и Operations

| Агент | Файл | Когда использовать |
|-------|------|-------------------|
| **DevOps Engineer** | `devops-engineer.md` | Docker, CI/CD, Helm, миграции |
| **DBA Engineer** | `dba-engineer.md` | Схема БД, SQL, индексы, ORM |

### Разработка

| Агент | Файл | Когда использовать |
|-------|------|-------------------|
| **SDE Backend** | `sde-backend.md` | Go код, handlers, repositories |
| **SDE Frontend** | `sde-frontend.md` | React, TypeScript, TanStack Query |

### Качество и Архитектура

| Агент | Файл | Когда использовать |
|-------|------|-------------------|
| **QA Engineer** | `qa-engineer.md` | Тесты, покрытие, моки, fixtures |
| **Architect** | `architect.md` | Дизайн API, схема БД, ADR |
| **System Analyst** | `system-analyst.md` | Спецификации, use cases, AC, бизнес-контекст |
| **Technical Writer** | `technical-writer.md` | Docs, Swagger, README, CHANGELOG |
| **Devil's Advocate** | `devils-advocate.md` | Edge cases, security, review |
| **CI Troubleshooter** | `ci-troubleshooter.md` | CI/CD диагностика |

### Когда делегировать агенту

- Задача требует специфических знаний (K8s, тестовые паттерны)
- Много однотипных операций (написать 10 тестов)
- Задача изолирована (не требует контекста всей беседы)
- Можно распараллелить (несколько независимых подзадач)

## Issues — источник истины

**Issues** в GitLab (`git.example.com/applications/dps150-web`) или GitHub (`github.com/applications/dps150-web`) — единственный источник истины по состоянию фичей, задач и техдолга. `.claude/plans/` — только для оперативного планирования внутри сессии. Подробности: `docs/issue-conventions.md`.

**Обязательные правила:**
1. **Новый план работ** — создать Issues, привязать к Milestone, назначить labels
2. **Начало работы** — назначить исполнителя (In Progress)
3. **Завершение задачи** — закрыть Issue с комментарием (ссылка на MR/коммит)
4. **Новый техдолг или баг** — создать Issue с label `tech-debt` / `bug`
5. **Изменение скоупа** — актуализировать Issues и Milestones

### RICE-оценка (обязательно для каждого нового Issue)

Команда из трёх агентов (параллельно, model: opus):
- **Architect** — техническая сложность, зависимости
- **Analyst** — пользовательская ценность, бизнес-дифференциация
- **Devil's Advocate** — скептическая оценка, риски переоценки

Параметры: Reach (1-5), Impact (0.25-3.0), Confidence (0-1), Effort (person-months).
Финальный RICE = `(avg_R * avg_I * avg_C) / avg_E`. Таблица с тремя оценками + среднее -> description Issue.

### Нумерация Issues

Формат: `PREFIX-NNN: Название`.

| Префикс | Label | Описание |
|---------|-------|----------|
| `F-NNN` | `feature` | Новая функциональность |
| `B-NNN` | `bug` | Баг / дефект |
| `TD-NNN` | `tech-debt` | Технический долг |
| `S-NNN` | `security` | Безопасность |
| `D-NNN` | `documentation` | Документация |
| `I-NNN` | `infra` | Инфраструктура |

Нумерация сквозная внутри класса. Счётчики: **F-001**, **TD-001**, **S-001**, **D-001**, **B-001**, **I-001**

### Labels и Milestones

**Labels:** `feature`, `bug`, `tech-debt`, `security`, `documentation`, `backend`, `frontend`, `bot`, `infra`

**Milestones:** Этап 1 PoC (active), Этап 2 MVP, Этап 3 (v1)

## Нефункциональные требования

### Качество
- Покрытие тестами: разумное, ориентир ~60% на handlers/core, без догмы
- Линтинг: golangci-lint (Go), ESLint (TypeScript)
- **Frontend `waitFor` timeout**: вызовы `waitFor` с async-мутациями — `{ timeout: 5000 }` (дефолт недостаточен для CI)

### Каскадность изменений

Любое изменение **обязано** сопровождаться обновлением связанных компонентов:
- **Тесты** — unit, integration-тесты, E2E
- **Frontend <-> Backend** — синхронизировать при изменении API-контрактов
- **Документация** — ARCHITECTURE.md, CHANGELOG.md, README.md
- **Схема БД** — `docs/database/schema.sql`


### Документация (обновлять каждую итерацию)
- **README.md** — актуализирует Technical Writer
- **CHANGELOG.md** — Keep a Changelog
- **docs/architecture/ARCHITECTURE.md** — source of truth
- **docs/database/schema.sql** — схема БД (source of truth: `backend/migrations/`)
- В завершение итерации — DevOps проверяет пайплайн, QA проверяет деплой

### Git-процесс — «почти-TBD»

Полный документ: `docs/git-workflow.md`.

| Ветка / тег        | Окружение      | Trigger                                  |
|--------------------|----------------|------------------------------------------|
| `master` / `main`  | **staging**    | Авто-деплой на push                      |
| `feature/<id>-<x>` | **review-app** | Эфемерное окружение, удаляется при close |
| `hotfix/<id>`      | **review-app** | Срочная дорожка к проду                  |
| `vX.Y.Z` (tag)     | **production** | Деплой только на push семантического тега |

- Master/main всегда production-ready (зелёный CI).
- Прямые push в master запрещены, только через MR/PR.
- Ветка живёт ≤ 3 дней. Длиннее — декомпозируй Issue.
- Production деплоится **только из тегов**, не из master.
- **Conventional Commits** обязательны (`docs/git-workflow.md`).
- **Branch protection rules** обязательны на сервере (см. там же).

### Definition of Ready / Done

Любая Issue имеет два жёстких чек-листа:
- **DoR** (`docs/definition-of-ready.md`) — Issue готова к старту.
- **DoD** (`docs/definition-of-done.md`) — Issue готова к закрытию.

Без зелёного DoD MR/PR не мерджится.

## ADR (Architecture Decision Records) — ОБЯЗАТЕЛЬНО

ADR хранятся в **Wiki проекта** (GitLab Wiki / GitHub Wiki). Полный
workflow — `docs/adr-workflow.md`.

**Когда ADR обязателен:** архитектурное решение, выбор технологии,
схема БД, API-контракт с альтернативами, security-модель, стратегия кэша,
интеграционный протокол, новый flow.

**Кто отвечает:**
- **Architect** — при любом архитектурном решении.
- **System Analyst** — при формализации спецификации с non-trivial выбором.

**Правило:** ADR публикуется **до или синхронно** с реализацией. Issue
не закрывается, пока ADR не опубликован в Wiki и не сослан в комментарии Issue/MR.

ADR — immutable после `Status: Accepted`. Изменение решения = новый ADR
+ перевод старого в `Superseded by ADR-MMM`.

Создание: `./scripts/adr-create.sh "<title>" > /tmp/adr.md && $EDITOR /tmp/adr.md && ./scripts/adr-create.sh --publish "ADR-NNN: <title>" /tmp/adr.md`.

## MCP-серверы

Полное описание — `docs/mcp-servers.md`. Конфиг — `.mcp.json` в корне проекта.

| Сервер              | Зачем                                                  |
|---------------------|--------------------------------------------------------|
| `serena`            | Семантический поиск/редактирование кода (LSP-aware)     |
| `context7`          | Актуальная документация библиотек/SDK                  |
| `memory`            | Persistent memory между сессиями (knowledge graph)     |
| `playwright`        | Браузерная автоматизация / E2E                         |
| `github`            | API-доступ к GitHub (Issues, PRs)                      |
| `gitlab`            | API-доступ к GitLab (Issues, MRs, Wiki)                |

> `sequential-thinking` MCP **исключён** из набора по соображениям безопасности.
> Для цепочек рассуждений используем встроенное extended thinking Claude 4.

## Дополнительные процессы

| Документ                          | Что описывает                                               |
|-----------------------------------|--------------------------------------------------------------|
| `docs/definition-of-ready.md`     | Когда Issue готова к старту                                  |
| `docs/definition-of-done.md`      | Когда Issue готова к закрытию                                |
| `docs/migration-safety.md`        | Expand-contract, online migrations, rollback                 |
| `docs/release-process.md`         | SemVer-теги, hotfix flow, release notes                      |
| `docs/agents-team-workflow.md`    | Workflow Agents Team: тимлид, DA-гейт, лимиты, checkpoints    |
| `docs/token-budget.md`            | Бюджеты токенов субагентов, эскалация, cost report            |
| `docs/superpowers-integration.md` | Claude Code superpowers + мультиагентная оркестрация          |
| `docs/da-self-review.md`          | DA-аудит boilerplate (пример отчёта)                         |
| `templates/`                      | Issue/MR templates, CODEOWNERS, commitlint, glossary          |
| `VERSION` + `CHANGELOG.md`        | Версия boilerplate (для `scripts/boilerplate-update.sh`)      |

## Контакты

- GitLab: `git.example.com/applications/dps150-web`
