# Skill: SDE Backend Engineer Agent

Специалист по разработке Go backend.

## Область ответственности

- Доменные сущности (entities)
- Репозитории и бизнес-логика
- HTTP handlers и middleware
- REST API endpoints
- Валидация и обработка ошибок

## Контекст проекта

**Стек:**
- Go 1.22+, Gin framework
- GORM (PostgreSQL)
- JWT аутентификация
- Clean Architecture

**Структура:**
```
backend/
├── cmd/api/main.go                 # Entry point
├── internal/
│   ├── config/                     # Viper конфигурация
│   ├── domain/
│   │   ├── entity/                 # Сущности
│   │   └── repository/             # Интерфейсы репозиториев
│   ├── infrastructure/
│   │   ├── cache/redis/            # Redis клиент
│   │   └── persistence/postgres/   # GORM репозитории
│   └── presentation/http/
│       ├── handler/                # HTTP handlers
│       ├── middleware/             # Auth, CORS, Logger
│       └── router/                 # Маршрутизация
└── pkg/
    ├── errors/                     # Обработка ошибок
    └── logger/                     # Zap логгер
```

## Паттерны кода

### Entity
```go
type {{Entity}} struct {
    BaseEntity
    Name    string     `gorm:"size:200;not null" json:"name"`
    FieldID *uuid.UUID `gorm:"type:uuid;index" json:"field_id,omitempty"`
    // Relations
    Field *Field `gorm:"foreignKey:FieldID" json:"field,omitempty"`
}

func ({{Entity}}) TableName() string { return "{{entities}}" }
```

### Repository Interface
```go
type {{Entity}}Repository interface {
    Create(ctx context.Context, e *entity.{{Entity}}) error
    GetByID(ctx context.Context, id uuid.UUID) (*entity.{{Entity}}, error)
    List(ctx context.Context, filter Filter, pagination Pagination) (*ListResult[entity.{{Entity}}], error)
    Update(ctx context.Context, e *entity.{{Entity}}) error
    Delete(ctx context.Context, id uuid.UUID) error
}
```

### Repository Implementation
```go
func (r *{{entity}}Repository) GetByID(ctx context.Context, id uuid.UUID) (*entity.{{Entity}}, error) {
    var e entity.{{Entity}}
    if err := r.db.WithContext(ctx).
        Preload("Field").
        First(&e, "id = ?", id).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, apperrors.ErrNotFound
        }
        return nil, err
    }
    return &e, nil
}
```

### Handler
```go
func (h *{{Entity}}Handler) GetByID(c *gin.Context) {
    id, err := uuid.Parse(c.Param("id"))
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
        return
    }
    e, err := h.repo.GetByID(c.Request.Context(), id)
    if err != nil {
        if errors.Is(err, apperrors.ErrNotFound) {
            c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
        return
    }
    c.JSON(http.StatusOK, h.toResponse(e))
}
```

## Команды разработки

```bash
cd backend
go run ./cmd/api          # Запуск
go build -o bin/api ./cmd/api  # Сборка
golangci-lint run ./...   # Линтинг
go mod tidy               # Зависимости
go test -v -race ./...    # Тесты
```

## Типичные задачи

1. **Новая entity** -> `internal/domain/entity/` + миграция
2. **Новый endpoint** -> handler method + route в router.go
3. **Новый репозиторий** -> interface + GORM implementation
4. **Валидация** -> binding tags в request struct
