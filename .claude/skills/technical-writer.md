# Skill: Technical Writer Agent

Специалист по документации — API docs, README, архитектурные документы.

## Область ответственности

- API документация (Swagger/OpenAPI)
- README и getting started
- Архитектурные документы (ADR)
- Диаграммы (C4, sequence, Mermaid)
- Changelog

## Контекст проекта

**Документация:**
```
project/
├── CLAUDE.md                       # Контекст для AI-агентов
├── README.md                       # Обзор проекта
├── CHANGELOG.md                    # История изменений
├── docs/
│   ├── architecture/
│   │   ├── ARCHITECTURE.md         # Архитектурное описание
│   │   └── ADR/                    # Architecture Decision Records
│   ├── api/
│   │   └── openapi.yaml            # OpenAPI spec
│   └── database/
│       └── schema.sql              # SQL схема
└── backend/
    └── docs/                       # Сгенерированный Swagger
```

## Паттерны документации

### Swagger Comments (Go/Gin)
```go
// @Summary      Получить элемент по ID
// @Description  Возвращает детальную информацию
// @Tags         items
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Item ID" format(uuid)
// @Success      200  {object}  ItemResponse
// @Failure      404  {object}  ErrorResponse
// @Security     BearerAuth
// @Router       /items/{id} [get]
```

### ADR Template
```markdown
# ADR-XXX: Название решения

## Статус
Принято | Отклонено | Заменено ADR-YYY

## Контекст
Описание проблемы или ситуации.

## Решение
Описание принятого решения.

## Последствия
### Положительные
### Отрицательные
### Риски

## Альтернативы
```

### CHANGELOG Format (Keep a Changelog)
```markdown
# Changelog

## [Unreleased]

### Added
- New feature description

### Changed
- Modified behavior

### Fixed
- Bug fix description

## [1.0.0] - YYYY-MM-DD

### Added
- Initial release
```

## Команды

```bash
# Генерация Swagger
cd backend && swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

# Просмотр Swagger UI: http://localhost:8080/swagger/index.html
```

## Типичные задачи

1. **Новый endpoint** -> Swagger comments + OpenAPI update
2. **Архитектурное решение** -> ADR документ
3. **Новая фича** -> CHANGELOG entry + README update
4. **Диаграмма** -> Mermaid / C4 model
