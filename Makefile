APP     := jobifai
BINARY  := ./$(APP)
MAIN    := ./cmd/server
IMAGE   := $(APP):latest
DB      := data/$(APP).db

.PHONY: all build run dev start test lint clean docker docker-run web-dev web-build e2e-server test-e2e test-e2e-ui

all: build

## build: compile the server binary
build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) $(MAIN)

## web-dev: start Vite dev server (proxies /api → Go on :8080)
web-dev:
	cd web && npm run dev

## web-build: build the React frontend into web/dist
web-build:
	cd web && npm run build

## run: build frontend + server then start (port 8080), killing any existing instance first
run: web-build build
	@pkill -f '$(BINARY)' 2>/dev/null || true
	@sleep 0.5
	$(BINARY)

## start: run backend + frontend dev server together (Ctrl-C stops both)
start:
	@trap 'kill 0' INT; \
	go run $(MAIN) & \
	cd web && npm run dev & \
	wait

dev:
	air

## test: run all tests
test:
	go test ./... -race -count=1

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## tidy: tidy and verify modules
tidy:
	go mod tidy
	go mod verify

## clean: remove build artefacts and local database
clean:
	rm -f $(BINARY)
	rm -f $(DB)

## docker: build the Docker image
docker:
	docker build -t $(IMAGE) .

## docker-run: run the Docker image with persistent volumes
docker-run:
	docker run --rm -it \
		-p 8080:8080 \
		-v "$$(pwd)/data:/app/data" \
		-v "$$(pwd)/job_applications:/app/job_applications" \
		-v "$$(pwd)/resume_style:/app/resume_style" \
		$(IMAGE)

## migrate-status: show current migration version
migrate-status: build
	@sqlite3 $(DB) "SELECT version FROM goose_db_version ORDER BY id DESC LIMIT 1;" 2>/dev/null || echo "no db yet"

## help: list targets with descriptions
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

## e2e-server: build Go binary and start server with a fresh test DB (used by Playwright webServer)
e2e-server:
	rm -f /tmp/e2e-test.db
	go build -trimpath -o /tmp/jobifai-e2e $(MAIN)
	JWT_SECRET=e2e-test-secret-do-not-use-in-prod /tmp/jobifai-e2e -db /tmp/e2e-test.db

## test-e2e: build frontend then run Playwright e2e tests
test-e2e: web-build
	cd web && npx playwright test

## test-e2e-ui: open Playwright interactive UI (local dev only)
test-e2e-ui: web-build
	cd web && npx playwright test --ui
