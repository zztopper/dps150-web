# Definition of Done (DoD)

Issue считается **закрытой** только когда **все** пункты ниже выполнены.
DoD — это финальный чек-лист, агрегирующий правила из остальных документов.
Применяется оркестратором (основным агентом) перед закрытием Issue.

## Чек-лист DoD

### 1. Реализация
- [ ] Все Acceptance Criteria из Issue выполнены **и явно проверены**
      (ссылки на тесты / ручные шаги в комментарии MR).
- [ ] Код соответствует архитектурным паттернам проекта (см. `docs/adr-workflow.md`).
- [ ] Никаких TODO/FIXME без сопоставленного Issue.
- [ ] Out of scope из Issue не нарушен (никаких «бонусов»).

### 2. Тесты (`docs/testing-pyramid.md`)
- [ ] Unit-coverage по затронутым модулям ~**60 %** (ориентир на core/handlers, без догмы).
- [ ] Каждый новый/изменённый endpoint имеет integration-тест.
- [ ] Каждая user-видимая фича имеет ≥ 1 E2E-сценарий (smoke + happy + ≥ 1 edge).
- [ ] Тесты не flaky: 2 повторных прогона зелёные.

### 3. Качество кода
- [ ] Linters / formatters зелёные локально и в CI.
- [ ] Type-check (`tsc -b` / `mypy` / etc.) зелёный.
- [ ] Зависимости проверены SCA / Dependabot (без known CVEs).

### 4. Архитектура и дизайн
- [ ] При архитектурном решении — ADR опубликован в Wiki проекта
      (`docs/adr-workflow.md`).
- [ ] LikeC4-модель обновлена при затрагиваемой архитектуре или новом флоу
      (`docs/likec4-workflow.md`).
- [ ] `npx likec4 validate` зелёный.

### 5. Документация
- [ ] CHANGELOG.md обновлён (`[Unreleased]` секция).
- [ ] README.md обновлён, если изменился пользовательский интерфейс / setup.
- [ ] OpenAPI / Swagger обновлён, если изменены эндпоинты.
- [ ] При breaking change — миграционная заметка в release notes.

### 6. Operations
- [ ] Миграции БД безопасны (см. `docs/migration-safety.md`):
      expand-contract, без долгих локов, с rollback.

### 7. Security
- [ ] Threat model обновлена для security-чувствительных изменений.
- [ ] Никаких секретов в коде / истории git.
- [ ] Аудит права доступа: новые endpoint'ы имеют корректные RBAC-проверки.
- [ ] Логирование не содержит PII.

### 8. Devil's Advocate gate
- [ ] DA-Task запущен в Agents Team.
- [ ] CRITICAL и HIGH устранены **в текущей сессии**.
- [ ] **Каждая** не-устранённая находка (MEDIUM/LOW/WISHES, pre-existing,
      out-of-scope) — оформлена как **новый Issue** в трекере.
- [ ] Verdict DA: **APPROVED**.

### 9. Pre-commit gate (`docs/pre-commit-gate.md`)
- [ ] `./scripts/precommit-check.sh` зелёный (build + lint + tests + likec4).
- [ ] Все комбинации `--quick`, `--full` (для security-чувствительных) прошли.

### 10. Git процесс
- [ ] Commit messages в формате Conventional Commits.
- [ ] Имя ветки `feature/<id>-<x>` / `bugfix/<id>-<x>` / `tech-debt/<id>-<x>`.
- [ ] Возраст ветки ≤ 3 дня (если больше — пересмотреть декомпозицию).
- [ ] MR/PR проходит CI полностью без `allow_failure: true` для тестов.
- [ ] Описание MR следует template'у с `Closes #NNN`.
- [ ] CODEOWNERS / required reviewers approve получены.

### 11. Закрытие Issue
- [ ] Issue в трекере переведён в `Closed` с комментарием:
      ссылка на MR, краткое summary изменений, ссылки на ADR и LikeC4.
- [ ] Если в работе обнаружены побочные находки — все они оформлены
      как новые Issues (TD-/B-/F-).

## Анти-паттерны

- ❌ «Это закроем потом» — потом не закрывается.
- ❌ «Тесты добавим в следующем спринте» — нарушение DoD; коммит запрещён.
- ❌ «DA APPROVED → можно push без precommit-check» — нет, оба гейта обязательны.
- ❌ «CHANGELOG обновлю в release-MR» — обновлять в feature-MR, не накапливать.
- ❌ Закрытие Issue без ссылки на MR — теряется аудит-след.

## Связь с DoR

DoR (`docs/definition-of-ready.md`) — Issue готова **войти**.
DoD — Issue готова **выйти**. Без хорошего DoR DoD недостижим: нельзя
проверить выполнение AC, которые сами размыты.
