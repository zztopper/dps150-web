# Testing Pyramid

Полная пирамида тестов — обязательное требование к любой реализации.
DA отказывает в approve коммиту/мерджу, если пирамида не покрыта.

```
            ╱──────╲
           ╱  E2E   ╲    ←  ключевые user flows (smoke + happy + critical edges)
          ╱──────────╲
         ╱ Integration╲   ←  все API endpoints, БД-операции, внешние интеграции
        ╱──────────────╲
       ╱     Unit       ╲  ←  ≥ 85% покрытия handlers/services/use-cases/domain
      ╱──────────────────╲
```

## 1. Unit-тесты — ≥ 85 % покрытия

**Что покрываем:** handlers, services, use-cases, domain logic, валидаторы,
утилиты, чистые функции, edge-cases в критических путях.

**Метрика покрытия:**
- **Минимальный порог:** 85 % line coverage по модулям бизнес-логики.
- Допустимые исключения: автогенерированный код, main/wire, миграции,
  thin glue без логики (явно отметить в `coverage-ignore` файле или
  через теги `//go:build ignore` / `/* istanbul ignore */`).
- Покрытие меряется в CI и публикуется в MR-комментарии.
  Падение покрытия ниже 85 % — блокер мерджа.

**Что НЕ нужно покрывать unit-тестами:**
- Подключение к реальной БД, Redis, S3 — это уровень integration.
- Полный HTTP-флоу с роутером — там же.
- Браузер, рендер UI — это E2E.

**Команды (адаптируйте под стек):**
```bash
# Go
make test-cover                              # вывод html/text покрытия
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1   # суммарный процент

# TypeScript / Vitest
npx vitest run --coverage
npx vitest run --coverage --reporter=verbose

# Python / pytest
pytest --cov=src --cov-report=term-missing --cov-fail-under=85
```

## 2. Integration-тесты — вся ключевая интеграционная функциональность

**Что покрываем:**
- Все REST/GraphQL endpoints (200/4xx/5xx ветки).
- Repository ↔ реальная БД (через testcontainers / Dockerized DB).
- Внешние интеграции (через wiremock / msw / VCR fixtures).
- Транзакции, миграции, idempotency.
- Auth/RBAC: проверки прав на каждом endpoint.
- Кросс-сервисные потоки внутри backend.

**Принципы:**
- **НЕ мокаем БД** — поднимаем реальную (testcontainers PostgreSQL/Redis).
- Каждый тест — изолированная транзакция или схема, чтобы не пересекаться.
- Покрытие — **по функциональности**, не процент. Считается через чек-лист:
  каждый endpoint и каждое use-case-сценарий имеет ≥ 1 integration-тест.
- DA проверяет, что список endpoint'ов в коде ↔ список покрытых тестами совпадает.

**Команды:**
```bash
make test-int                            # полный прогон
make test-int FILTER=auth                # подмножество
```

## 3. E2E-тесты — все ключевые user flows

**Что покрываем:**
- Smoke: главная страница загружается, login работает.
- Happy paths по каждой ключевой фиче (см. AC в Issues).
- Critical edges: 401/403, валидации форм, error states.
- Cross-page navigation, состояние после refresh.

**Инструмент:** Playwright (есть через MCP-сервер) или Cypress.
**Окружение:** эфемерный dev-environment ветки (см. `docs/git-workflow.md`).

**Принципы:**
- Тесты идемпотентны и независимы (любой порядок).
- Для CI: `--workers=2..4`, чтобы укладываться в разумное время.
- Скриншоты/видео при падении сохраняются как артефакты MR.
- Каждая Issue с user-видимой фичей **обязана** добавить хотя бы один
  E2E-сценарий (закрепляется в AC).

**Команды:**
```bash
npx playwright test                          # все
npx playwright test --grep "login"           # фильтр
npx playwright test --ui                     # интерактивно (локально)
```

## Покрытие функциональности

Помимо процентного покрытия unit, обязательно покрытие по сценариям.
Шаблон в описании Issue:

```markdown
## Test Coverage
- Unit: <list of модулей> — целевое покрытие ≥ 85 %
- Integration:
  - [ ] POST /resources — happy path
  - [ ] POST /resources — 400 validation
  - [ ] POST /resources — 401 unauth
  - [ ] POST /resources — 403 forbidden
- E2E:
  - [ ] Создание ресурса через UI
  - [ ] Edit + autosave
  - [ ] Удаление с подтверждением
```

Перед merge все чек-боксы должны быть закрыты, либо явно отмечен отказ
с обоснованием (только для LOW/WISHES, никогда для CRITICAL/HIGH).

## Гейтинг

DA в финальной верификации проверяет:
- [ ] Unit-coverage ≥ 85 % по затронутым модулям. Падение покрытия — блокер.
- [ ] Каждый новый/изменённый endpoint имеет integration-тест.
- [ ] Каждая user-видимая фича имеет хотя бы один E2E-сценарий.
- [ ] Все три уровня запускаются в CI на feature-ветке. Падение — блокер.
- [ ] Тесты не flaky (повторный прогон зелёный).

## Tier-2 testing — обязательно для critical business logic

Стандартная пирамида (unit/integration/E2E) — **минимум**. Для модулей,
помеченных как **critical** (биллинг, auth, RBAC, расчётные движки,
бизнес-инварианты), дополнительно применяются:

### Mutation testing
- **Зачем**: проверка качества тестов, не покрытия. Вводит мутации в код,
  тесты должны их ловить.
- **Tools**:
  - Go: `gremlins`, `go-mutesting`.
  - TypeScript/JavaScript: `Stryker`.
  - Python: `mutmut`, `cosmic-ray`.
  - Rust: `cargo-mutants`.
- **Цель**: ≥ 80 % mutation score на critical modules.
- **Запуск**: nightly CI или вручную перед релизом критичной фичи (не на
  каждом MR — занимает много времени).
- **Гейтинг**: падение ниже 80 % на critical modules — Issue с приоритетом.

### Property-based testing
- **Зачем**: проверка инвариантов на больших диапазонах входных данных.
  Особенно ценно для парсеров, валидаторов, расчётных функций, сериализации.
- **Tools**:
  - Go: `gopter`, `rapid`.
  - TypeScript/JavaScript: `fast-check`.
  - Python: `hypothesis`.
  - Rust: `proptest`, `quickcheck`.
- **Что покрываем**: round-trip (encode/decode), идемпотентность, монотонность,
  алгоритмические инварианты.
- **Запуск**: в составе обычного `make test` — не нагружает время значительно.

### Contract testing (consumer-driven)
- **Зачем**: проверка интеграционных контрактов между микросервисами /
  внешними API.
- **Tools**: `Pact`, `Spring Cloud Contract`.
- **Применение**: если в проекте >1 микросервиса или активно взаимодействие
  с внешним API.

### Когда обязательно

Модуль помечен как **critical** в `docs/critical-modules.md` (создаётся
архитектором при необходимости). Признаки critical:
- Финансовые операции / биллинг.
- Auth / RBAC / authorization.
- Расчёт скидок, налогов, комиссий.
- Криптография, токены, подписи.
- Бизнес-инварианты, нарушение которых = data corruption.

Для critical-модулей DA в финальном ревью **обязан** проверить:
- [ ] Mutation score ≥ 80 % за последний nightly.
- [ ] Property-based tests покрывают ключевые инварианты.
- [ ] Contract tests зелёные (если применимо).

## Анти-паттерны

- ❌ Подменять integration unit-тестами с моками БД.
- ❌ Считать «покрытие 85 %» через тривиальные тесты на геттеры.
- ❌ Skipping E2E «потом сделаем».
- ❌ Один E2E-сценарий на всю фичу. Минимум: smoke + happy + 1-2 edges.
- ❌ Тесты, зависящие от прошлого state БД или порядка запуска.

## CI-якорь

Pipeline (концептуально):
```
lint → unit + coverage gate (85 %) → build → integration → deploy to dev → E2E → DA-review → mergeable
```

Каждый шаг — обязательный, никаких `allow_failure: true` для тестов в pre-merge стадии.
