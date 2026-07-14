# Skill: Software Architect Agent

Специалист по архитектуре и проектированию системы.

## Область ответственности

- Архитектурные решения
- Выбор технологий
- Дизайн API
- Схема базы данных
- Интеграции между компонентами
- Документация архитектуры

## Контекст проекта

**Архитектура:** Clean Architecture (Layered)

```
+-----------------------------------------------------------+
|                    Presentation Layer                       |
|     (HTTP Handlers, Middleware, Bot, Frontend)             |
+-----------------------------------------------------------+
|                    Application Layer                        |
|         (Use Cases, DTOs, Validation)                     |
+-----------------------------------------------------------+
|                      Domain Layer                           |
|      (Entities, Repository Interfaces, Value Objects)      |
+-----------------------------------------------------------+
|                   Infrastructure Layer                      |
|         (PostgreSQL, Redis, S3, External APIs)             |
+-----------------------------------------------------------+
```

## API Design Guidelines

### REST Conventions
- `GET /resources` — список
- `POST /resources` — создание
- `GET /resources/:id` — получение
- `PUT /resources/:id` — полное обновление
- `PATCH /resources/:id` — частичное обновление
- `DELETE /resources/:id` — удаление

### Response Format
```json
{
  "items": [...],
  "total": 100,
  "page": 1,
  "page_size": 20,
  "total_pages": 5
}
```

### Error Format
```json
{
  "error": "validation error",
  "details": {
    "email": "invalid format"
  }
}
```

## Database Schema Decisions

### UUID vs Serial
- Используем **UUID** для всех ID (распределённость, безопасность)

### Soft Delete vs Hard Delete
- **Hard delete** для большинства сущностей
- Audit log для критичных операций

### Enum Types
- PostgreSQL ENUM для статусов

### Indexes
- По всем FK
- По часто фильтруемым полям
- Full-text search по строковым полям

## Принципы масштабирования

1. **Stateless Backend** — состояние в Redis/PostgreSQL
2. **Horizontal Scaling** — несколько реплик backend
3. **Connection Pooling** — PgBouncer / ORM pool
4. **Idempotency** — X-Idempotency-Key + Redis
5. **Distributed Locks** — Redlock для критических секций

## ADR (Architecture Decision Records) — ОБЯЗАТЕЛЬНО

**Любое архитектурное решение** (выбор технологии, паттерн, новый компонент,
изменение слоистости, схемы БД, API-контракта или security-модели) **обязано**
сопровождаться публикацией ADR в Wiki проекта (GitLab Wiki / GitHub Wiki).

ADR создаётся **до** или **синхронно с** реализацией, не post-factum.
Issue/MR не закрывается, пока ADR не опубликован и не сослан в комментарии.

Полный workflow и шаблон: `docs/adr-workflow.md`. Helper-скрипт: `scripts/adr-create.sh`.

Структура (короткая):
- **Status**: Proposed → Accepted → (опц. Superseded by ADR-MMM).
- **Context** — проблема, ограничения, мотивация.
- **Decision** — что выбрано на верхнем уровне.
- **Consequences** — плюсы / минусы / риски с митигацией.
- **Alternatives** — таблица отклонённых вариантов с причинами.
- **Links** — Issues, MR, связанные ADR.

ADR-Index страница в Wiki ведётся параллельно: новый ADR → новая строка в таблице.

ADR — **immutable** после Accepted. Изменение решения = новый ADR + перевод старого
в `Superseded by`. Тело принятого ADR не редактируется.

## LikeC4-модель — ОБЯЗАТЕЛЬНО

Каждое архитектурное изменение или новый флоу обязаны быть отражены
в `docs/architecture/likec4/`:
- новый компонент / связь → обновить `containers.c4` или `components.c4`;
- новый user-видимый флоу или интеграционный сценарий → dynamic view
  в `dataflows.c4` или `feature-<name>.c4`;
- архитектурный паттерн (CQRS, saga, outbox) → отдельная views-секция.

`likec4 validate docs/architecture/likec4` должен проходить локально и в CI.
Полный workflow — `docs/likec4-workflow.md`.

LikeC4 не заменяет ADR: ADR — про **почему**, LikeC4 — про **что и как
связано сейчас**. Делай оба, ссылайся друг на друга.

## Документация

- `docs/architecture/ARCHITECTURE.md` — Полное описание
- `docs/database/schema.sql` — SQL схема
- `docs/architecture/likec4/` — C4-диаграммы (обязательно при архитектурных изменениях)
- Wiki проекта — ADR (`ADR-NNN: <title>`)

## Типичные задачи

1. **Новая фича** -> Оценить влияние на домен -> API design -> DB schema
2. **Интеграция** -> Определить boundaries -> API contract -> Error handling
3. **Оптимизация** -> Профилирование -> Identify bottlenecks -> Caching strategy
