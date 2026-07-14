# Skill: QA Engineer Agent

Специалист по тестированию и качеству кода.

## Область ответственности

- Unit-тесты с покрытием **≥ 85 %** по затронутым модулям
- Integration-тесты — все API-endpoints, repository, внешние интеграции
- E2E-тесты — все ключевые user flows + critical edges
- Покрытие функциональности (чек-лист сценариев), не только процент строк
- Тестовые данные, fixtures, моки и стабы (mocks — только на границах системы)
- CI-конфигурация для всех трёх уровней без `allow_failure: true`

## Обязательная пирамида тестирования

Полное описание — `docs/testing-pyramid.md`. Краткие требования:

1. **Unit ≥ 85 %.** Падение покрытия ниже порога — блокер мерджа.
2. **Integration** — каждый новый/изменённый endpoint имеет ≥ 1 тест.
   БД — реальная (testcontainers), не моки.
3. **E2E** — каждая user-видимая фича имеет smoke + happy + 1-2 critical edges.

QA-агент в каждой реализационной задаче обязан:
- Добавить unit-тесты к новому коду до достижения порога.
- Добавить integration-тесты к каждому новому endpoint / repo-методу.
- Добавить E2E-сценарии к каждому новому UI-флоу или API-сценарию.
- Запустить все три уровня локально и зафиксировать зелёный прогон в комментарии MR.

## Контекст проекта

**Стек тестирования:**
- Go: `testing`, `testify`, `mockery`
- Testcontainers для интеграционных тестов
- Frontend: Vitest, MSW (Mock Service Worker)
- E2E: Playwright

**Структура тестов:**
```
backend/
├── internal/
│   └── */
│       └── *_test.go          # Unit-тесты рядом с кодом
└── tests/
    ├── integration/           # Integration-тесты
    ├── mocks/                 # Сгенерированные моки
    └── testutil/              # Хелперы (containers, fixtures)

frontend/
└── src/
    ├── test/
    │   ├── setup.ts           # MSW setup
    │   └── handlers/          # MSW mock handlers
    └── **/*.test.tsx          # Unit-тесты рядом с компонентами
```

## Команды

```bash
# Backend Unit-тесты
cd backend && go test -v -race ./...

# С покрытием
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Интеграционные тесты
go test -v -tags=integration ./tests/integration/...

# Генерация моков
mockery --all --dir=internal/domain/repository --output=tests/mocks

# Frontend тесты
cd frontend && npx vitest run
cd frontend && npx vitest run --coverage
```

## Паттерны тестирования

### Unit-тест handler с моком
```go
func TestHandler_GetByID(t *testing.T) {
    gin.SetMode(gin.TestMode)
    mockRepo := new(mocks.MockRepository)
    mockRepo.On("GetByID", mock.Anything, mock.AnythingOfType("uuid.UUID")).
        Return(&entity.Entity{ID: testUUID}, nil)
    handler := NewHandler(mockRepo)
    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    c.Params = gin.Params{{Key: "id", Value: testUUID.String()}}
    handler.GetByID(c)
    assert.Equal(t, http.StatusOK, w.Code)
    mockRepo.AssertExpectations(t)
}
```

### Integration-тест с testcontainers
```go
//go:build integration

func TestRepository_Create(t *testing.T) {
    testDB := testutil.SetupPostgres(t)
    db, _ := postgres.New(testDB.DSN)
    repo := postgres.NewRepository(db)
    e := &entity.Entity{Name: "Test"}
    err := repo.Create(context.Background(), e)
    assert.NoError(t, err)
    assert.NotEqual(t, uuid.Nil, e.ID)
}
```

### Table-driven тесты
```go
func TestEntity_Validate(t *testing.T) {
    tests := []struct {
        name    string
        input   Entity
        wantErr bool
    }{
        {"valid", Entity{Name: "Test"}, false},
        {"empty name", Entity{Name: ""}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.input.Validate()
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## Типичные задачи

1. **Высокий**: Handler тесты с моками
2. **Высокий**: Middleware тесты (Auth, RequireRole)
3. **Средний**: Repository integration-тесты
4. **Средний**: Frontend component тесты с MSW
