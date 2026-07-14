# LikeC4 — обязательная архитектурная модель

LikeC4 — текстовый формат описания C4-моделей с генерацией интерактивных
диаграмм. Хранится в `docs/architecture/likec4/`. Модель — **исполняемая
документация архитектуры**: одна точка истины для архитекторов, разработчиков,
DA и stakeholders.

## Правило коммита

**Каждый коммит, который меняет архитектуру или флоу, обязан обновить
LikeC4-модель в том же MR.**

Что считается «меняет архитектуру или флоу»:
- новый компонент (сервис, бот, воркер, БД, кэш, очередь);
- удаление/слияние компонентов;
- новая связь (REST вызов, gRPC, очередь, общий стор);
- изменение протокола или направления потока;
- новая фича с user-видимым флоу (цепочка экранов / API-вызовов);
- архитектурный паттерн (CQRS, saga, outbox, retry-стратегия).

Что **не** требует обновления:
- баг-фикс без изменения связей;
- рефакторинг внутри одного компонента;
- косметика, тесты, docs без архитектурных последствий.

DA в pre-commit ревью **обязан** проверить: «затронута ли архитектура?»
и, если да, проверить наличие соответствующих изменений в `*.c4` и в Issue/MR.

## Структура файлов

```
docs/architecture/likec4/
├── landscape.c4                # System landscape: внешние акторы, system context
├── containers.c4               # C2: контейнеры внутри системы
├── components.c4               # C3: компоненты внутри ключевых контейнеров
├── code-level.c4               # C4: ключевые модули/классы (не для всех)
├── dataflows.c4                # Динамические views: основные flows
├── dataflows-extended.c4       # Динамические views: edge-флоу, error-paths
└── feature-<name>.c4           # Feature-views под конкретные фичи
```

Минимум для нового проекта: `landscape.c4` + `containers.c4`. Остальное
добавляется по мере роста.

## Шаблон landscape.c4 (стартовый)

```likec4
specification {
  element actor
  element system
  element externalSystem
}

model {
  user = actor 'User' {
    description 'Конечный пользователь системы'
  }

  system MyApp 'My Application' {
    description 'Описание системы из 1-2 предложений'
  }

  externalSystem identityProvider 'Identity Provider'

  user -> MyApp 'использует через web/mobile'
  MyApp -> identityProvider 'аутентификация (OAuth2)'
}

views {
  view landscape {
    title 'System Landscape'
    include *
    autoLayout
  }
}
```

## Шаблон containers.c4

```likecу4
specification {
  element container
}

model {
  extend MyApp {
    container web 'Web Frontend' {
      technology 'React, TypeScript'
    }
    container api 'API Backend' {
      technology 'Go, REST'
    }
    container db 'Database' {
      technology 'PostgreSQL'
    }

    web -> api 'JSON/HTTPS'
    api -> db 'SQL/TLS'
  }
}

views {
  view containers of MyApp {
    title 'Containers'
    include *
    autoLayout
  }
}
```

## Динамический view (флоу)

```likec4
views {
  dynamic view loginFlow {
    title 'User Login Flow'
    user -> web 'открывает /login'
    web -> api 'POST /auth/login'
    api -> identityProvider 'verify credentials'
    identityProvider -> api 'JWT'
    api -> web 'JWT cookie'
    web -> user 'redirect /dashboard'
    autoLayout
  }
}
```

Каждая user-видимая фича получает либо отдельный dynamic view в
`feature-<name>.c4`, либо вкладывается в `dataflows.c4` / `dataflows-extended.c4`.

## Команды

```bash
# Локальный preview с горячей перезагрузкой
npx likec4 dev docs/architecture/likec4

# ВАЛИДАЦИЯ — ОБЯЗАТЕЛЬНО после любого изменения .c4 файлов
npx likec4 validate docs/architecture/likec4

# Генерация статических ассетов (PNG/SVG для Wiki/README)
npx likec4 export docs/architecture/likec4 --output build/c4
```

`npx likec4 ...` не требует глобальной установки — пакет подтягивается из npm на лету.
Для постоянной работы можно поставить глобально: `npm install -g likec4`.

## Workflow обновления модели

1. **Architect / SDE при реализации фичи:**
   - Обновляет `containers.c4` / `components.c4`, если добавлен компонент или связь.
   - Создаёт/дополняет dynamic view в `dataflows.c4` или `feature-<name>.c4`.
   - **ОБЯЗАТЕЛЬНО** запускает `npx likec4 validate docs/architecture/likec4`
     локально. Зелёный выхлоп — необходимое условие коммита (см. `docs/pre-commit-gate.md`).
2. **Technical Writer:**
   - Делает экспорт PNG/SVG, если они вшиты в README/Wiki.
   - Поддерживает индекс в Wiki (страница `Architecture` со ссылками).
3. **DA в ревью:**
   - Проверяет, что соответствующий `.c4` файл изменён в этом же MR.
   - Проверяет, что `npx likec4 validate docs/architecture/likec4` зелёный.
     Любая ошибка валидации — CRITICAL до коммита.
   - Если флоу новый — проверяет наличие dynamic view.
   - При архитектурном решении — проверяет наличие ADR (см. `docs/adr-workflow.md`)
     **и** соответствующего изменения в LikeC4.

## Анти-паттерны

- ❌ «Допишу диаграмму потом» — потом никто не дописывает.
- ❌ Один view на 50 элементов — нечитаемо. Разделяй по контексту.
- ❌ LikeC4 как замена ADR — это разные артефакты:
  - ADR хранит **почему** и **альтернативы**;
  - LikeC4 показывает **что** и **как связано** в текущем состоянии.
  Делай оба, ссылайся друг на друга.

## CI-проверка

```yaml
# Концептуально для GitLab CI / GitHub Actions
likec4:validate:
  image: node:20-alpine
  script:
    - npm install -g likec4
    - likec4 validate docs/architecture/likec4
  rules:
    - changes:
        - docs/architecture/likec4/**/*
```

Дополнительно — в pre-commit (см. `docs/pre-commit-gate.md`) и в DA-чек-листе.
