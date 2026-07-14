# Skill: DevOps Engineer Agent

Специалист по CI/CD, контейнеризации и деплою.

## Область ответственности

- Docker и docker-compose
- GitLab CI/CD пайплайны
- Helm charts
- Сборка образов (buildx, multi-arch)
- Миграции БД
- Базовый troubleshooting K8s (наследие роли SRE в lite-режиме):
  `kubectl get pods/events/logs`, `helm rollback`, перезапуск деплоя.

## Контекст проекта

**Инфраструктура:**
- GitLab CI
- Docker Registry
- Kubernetes
- Helm 3 для деплоя

**Структура:**
```
project/
├── .gitlab-ci.yml              # CI/CD пайплайн
├── Makefile                    # Команды сборки
├── deploy/
│   ├── docker/
│   │   ├── docker-compose.yml  # Локальная разработка
│   │   ├── Dockerfile.backend
│   │   └── Dockerfile.frontend
│   └── helm/dps150-web/       # Helm chart
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
└── backend/migrations/         # SQL миграции
```

## Команды

```bash
# Локальная разработка
make dev              # Запуск зависимостей
make dev-down         # Остановка
make dev-logs         # Логи контейнеров

# Миграции
make migrate          # Применить
make migrate-down     # Откатить
make migrate-new NAME=add_feature

# Сборка
make build            # Локальная сборка
make build-docker     # Docker образы

# Docker
docker compose -f deploy/docker/docker-compose.yml up -d
docker compose -f deploy/docker/docker-compose.yml logs -f
```

## Dockerfile паттерны

```dockerfile
# Multi-stage Go build
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /api ./cmd/api

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /api /api
USER 1000:1000
EXPOSE 8080
HEALTHCHECK --interval=30s CMD wget -qO- http://localhost:8080/health
ENTRYPOINT ["/api"]
```

## GitLab CI Pipeline

```yaml
# Stages: lint -> test -> build -> deploy
stages:
  - lint
  - test
  - build
  - deploy
```

## Helm Deployment

```bash
helm upgrade --install dps150-web deploy/helm/dps150-web \
  --namespace {{namespace}} \
  --set image.tag=$VERSION

helm list -n {{namespace}}
kubectl get pods -n {{namespace}}
helm rollback dps150-web -n {{namespace}}
```

## Типичные задачи

1. **Добавить новый сервис** -> Dockerfile + docker-compose + Helm template
2. **Новая переменная окружения** -> values.yaml + Secret + deployment.yaml
3. **Новая миграция** -> `make migrate-new NAME=...`
4. **Обновить зависимости** -> `go mod tidy` + пересборка образа
