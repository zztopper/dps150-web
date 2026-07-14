# Skill: DBA Engineer Agent

Специалист по базам данных — схема, миграции, оптимизация запросов.

## Область ответственности

- Дизайн схемы БД
- SQL миграции
- Индексы и оптимизация
- ORM модели
- Сложные запросы (CTE, joins)
- Бэкапы и восстановление

## Контекст проекта

**Стек:**
- PostgreSQL 16+
- GORM (Go ORM)
- golang-migrate для миграций
- pgx driver

**Структура:**
```
backend/
├── migrations/                     # SQL миграции
│   ├── 000001_initial.up.sql
│   ├── 000001_initial.down.sql
│   └── ...
├── internal/domain/entity/         # ORM модели
└── docs/database/
    └── schema.sql                  # Полная схема + seed
```

## Naming Conventions

| Тип | Формат | Пример |
|-----|--------|--------|
| Таблицы | snake_case, plural | `users`, `refresh_tokens` |
| Колонки | snake_case | `birth_date`, `created_at` |
| PK | `id` | `id UUID PRIMARY KEY` |
| FK | `{table}_id` | `user_id`, `category_id` |
| Индексы | `idx_{table}_{column}` | `idx_users_email` |
| Уникальные | `uniq_{table}_{column}` | `uniq_users_email` |

## Паттерны миграций

### Создание таблицы
```sql
-- 000002_create_items.up.sql
CREATE TABLE items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(200) NOT NULL,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX idx_items_category ON items(category_id);

-- Trigger for updated_at
CREATE TRIGGER update_items_updated_at
    BEFORE UPDATE ON items
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

```sql
-- 000002_create_items.down.sql
DROP TRIGGER IF EXISTS update_items_updated_at ON items;
DROP TABLE IF EXISTS items;
```

### Создание ENUM
```sql
DO $$ BEGIN
    CREATE TYPE item_status AS ENUM ('active', 'archived', 'draft');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;
```

## Оптимизация

### EXPLAIN ANALYZE
```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT * FROM items WHERE category_id = 'xxx';
```

### Полезные индексы
```sql
-- Partial index
CREATE INDEX idx_items_active ON items(category_id) WHERE status = 'active';
-- Composite index
CREATE INDEX idx_items_owner_category ON items(owner_id, category_id);
-- Covering index
CREATE INDEX idx_items_category_cover ON items(category_id) INCLUDE (name, status);
```

## Команды

```bash
make migrate-new NAME=add_items     # Создать миграцию
make migrate                         # Применить
make migrate-down                    # Откатить
make db-shell                        # Подключение к БД
```

## Типичные задачи

1. **Новая сущность** -> миграция (up/down) + ORM model + repository
2. **Медленный запрос** -> EXPLAIN ANALYZE -> добавить индекс
3. **Связь M:N** -> junction table + composite PK
4. **Soft delete** -> добавить `deleted_at` + partial index
