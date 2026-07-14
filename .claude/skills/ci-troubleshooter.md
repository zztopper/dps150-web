# Skill: CI Troubleshooter Agent

Специализированный агент для диагностики и исправления проблем CI/CD pipeline в GitLab.

## Область ответственности

- Анализ failed jobs в GitLab CI
- Диагностика причин падения pipeline
- Исправление .gitlab-ci.yml
- Исправление проблем с зависимостями (go.mod, package.json)
- Исправление проблем с Docker builds
- Исправление проблем с Helm charts

## Инструменты

- `glab` CLI для работы с GitLab API
- Чтение логов jobs через API
- Редактирование CI конфигурации
- Локальное тестирование (lint, build, test)

## Процесс диагностики

### 1. Получить статус pipeline
```bash
glab ci status --repo applications/dps150-web
```

### 2. Получить список jobs
```bash
glab api "/projects/applications%2Fdps150-web/pipelines/{PIPELINE_ID}/jobs"
```

### 3. Получить логи failed job
```bash
glab api "/projects/applications%2Fdps150-web/jobs/{JOB_ID}/trace"
```

### 4. Анализ ошибки

Типичные ошибки и решения:

#### Go версия несовместима
```
Error: go.mod requires go >= X.Y
```
**Решение:** Обновить версию Go в go.mod или Docker image

#### golangci-lint версия
```
Error: the Go language version used to build golangci-lint is lower
```
**Решение:** Использовать более новую версию golangci-lint

#### Docker image не найден
```
Error: manifest not found
```
**Решение:** Использовать существующий tag образа

#### NPM ошибки
```
Error: npm ERR!
```
**Решение:** Проверить package.json, package-lock.json, версии зависимостей

### 5. Локальная проверка перед push

```bash
# Go lint
cd backend && golangci-lint run --timeout 5m ./...

# Frontend lint
cd frontend && npm run lint && npx tsc -b

# Helm lint
helm lint deploy/helm/dps150-web

# Go tests
cd backend && go test -v ./...

# Frontend tests
cd frontend && npx vitest run
```

## Чеклист перед push

- [ ] `golangci-lint run ./...` проходит локально
- [ ] `npm run lint && npx tsc -b` проходит локально
- [ ] `helm lint` проходит локально
- [ ] Версии Go согласованы (go.mod, CI images)
- [ ] Все необходимые файлы созданы (templates, configs)
- [ ] CHANGELOG.md обновлён
- [ ] Swagger актуализирован
