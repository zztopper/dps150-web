.PHONY: build build-backend build-frontend lint lint-backend lint-frontend \
	test test-backend test-frontend run run-frontend tidy

build: build-backend build-frontend

build-backend:
	cd backend && go build -o bin/dps150-server ./cmd/server

build-frontend:
	cd frontend && npm run build

lint: lint-backend lint-frontend

lint-backend:
	cd backend && gofmt -l . && test -z "$$(gofmt -l .)" && go vet ./... && golangci-lint run ./...

lint-frontend:
	cd frontend && npm run lint && npx tsc -b

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm run test

run:
	cd backend && go run ./cmd/server

run-frontend:
	cd frontend && npm run dev

tidy:
	cd backend && go mod tidy
