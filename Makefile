.PHONY: build build-backend build-frontend embed-frontend release-binaries \
	lint lint-backend lint-frontend \
	test test-backend test-frontend run run-frontend tidy

WEBUI_DIST := backend/internal/webui/dist
RELEASE_PLATFORMS := darwin/arm64 linux/amd64 linux/arm64

build: build-backend

# Single binary: the production frontend bundle is embedded via go:embed
# (backend/internal/webui). Without the bundle the binary still builds and
# serves the API only.
build-backend: embed-frontend
	cd backend && CGO_ENABLED=0 go build -o bin/dps150-server ./cmd/server

build-frontend:
	cd frontend && npm run build

# Copy the built frontend into the webui package for go:embed. .gitkeep is
# recreated so the placeholder committed to git survives the cleanup.
embed-frontend: build-frontend
	rm -rf $(WEBUI_DIST)
	mkdir -p $(WEBUI_DIST)
	cp -R frontend/dist/. $(WEBUI_DIST)/
	touch $(WEBUI_DIST)/.gitkeep

# Cross-compiled single binaries with the frontend embedded. Pure-Go SQLite
# (glebarez/sqlite) keeps CGO off, so plain GOOS/GOARCH switching works.
release-binaries: embed-frontend
	cd backend && for p in $(RELEASE_PLATFORMS); do \
		echo "building bin/dps150-server-$${p%/*}-$${p#*/}"; \
		GOOS=$${p%/*} GOARCH=$${p#*/} CGO_ENABLED=0 \
			go build -trimpath -ldflags="-s -w" \
			-o bin/dps150-server-$${p%/*}-$${p#*/} ./cmd/server || exit 1; \
	done

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
