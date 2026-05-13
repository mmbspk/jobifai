# jobifai — Developer Context

## What This Project Is

AI-powered application automation tool. Users configure their profile and preferences; the tool searches job boards, evaluates listings against the profile using an LLM, fills application forms automatically, and tracks outcomes. Supports any profession — never assume it is for software/IT roles.

## Tech Stack

### Backend (Go)
- Module: `github.com/user/jobifai` — Go 1.25
- HTTP router: `go-chi/chi/v5`
- Database: SQLite via `modernc.org/sqlite` (pure Go, no CGO) + `pressly/goose/v3` for migrations
- Logging: `rs/zerolog` — **always use `zerolog`, never `fmt.Println` or `log.*` from stdlib**
- Auth: `golang-jwt/jwt/v5` (Bearer tokens), `golang.org/x/crypto/bcrypt` for passwords
- Browser automation: `go-rod/rod`
- LLM: internal multi-provider client (`internal/llm/`) supporting Claude, OpenAI-compatible, Ollama
- Settings/secrets: SQLite-backed key-value store with AES-GCM encrypted secrets (`internal/config/`)
- OpenAPI codegen: `ogen-go/ogen` — generated code lives in `gen/`, **do not edit manually**

### Frontend (React/TypeScript)
- React 19, TypeScript 5, Vite 8
- Styling: Tailwind CSS v4 (CSS-variable theming, dark mode via `.dark` class on `<html>`)
- Component primitives: Radix UI
- Server state: TanStack React Query v5
- Routing: React Router v7
- **No global state store** (no Redux, no Zustand, no Context for app state)
- API client: all requests go through `web/src/api/client.ts` with Bearer JWT from `localStorage`

## Package Layout

```
cmd/server/main.go          — dependency wiring + HTTP server start
internal/domain/types.go    — canonical data model (source of truth; TypeScript mirror: web/src/types.ts)
internal/domain/form.go     — form-filling domain types
internal/handler/
  handler.go                — Services struct (DI bag) + all interface definitions
  router.go                 — complete route map
  *.go                      — handler groups (auth, bot, jobs, resume, settings, users, ws, vnc, usage)
internal/auth/              — JWT management, middleware, UserStore, Google OAuth
internal/bot/               — automation orchestrator; platform runners registered via init()
internal/llm/               — multi-provider LLM client with retry + per-user usage tracking
internal/resume/            — LLM-powered scoring, tailoring, halal checking, PDF rendering
internal/config/            — SQLite k/v ConfigStore + AES-GCM SecretsStore
internal/db/                — database open + goose migration runner
internal/browser/           — Rod browser manager + cookie session store
internal/scraper/           — job detail fetching
internal/storage/           — (reserved, currently empty)
internal/ws/                — WebSocket broadcaster for live log streaming
gen/                        — ogen-generated types (do not edit)
web/src/
  api/                      — typed fetch helpers (one file per domain)
  components/               — shared UI components
  pages/                    — route-level page components
  hooks/                    — custom React hooks
  types.ts                  — TypeScript mirror of domain/types.go
```

## Key Architectural Patterns

- **Dependency injection**: All handlers receive deps through `*Services` — no package-level globals.
- **Interface discipline**: `Services` fields are interfaces (`ConfigStore`, `SecretsStore`, `ResumeTailor`, `JobEvaluator`, etc.) defined in `handler/handler.go`. Handlers must never import concrete implementations directly.
- **Bot runner registry**: Platform runners (`internal/bot/`) are registered via `registerRunner` + package `init()`, following an open/closed pattern.
- **LLM provider dispatch**: Runtime selection via `domain.LLMConfig.Provider` string. Factory functions (`LLMFactory`, `EvaluatorFactory`, etc.) on `Services` build fresh clients per request so config changes take effect without restart.
- **User-scoped data**: Every DB operation is keyed by `user_id`. There is no concept of global state in the data layer.
- **React Query cache keys**: Query keys are the source of truth for cache invalidation — always invalidate the narrowest key possible.

## Makefile Commands

```
make build          — compile Go binary
make web-build      — compile React frontend → web/dist
make web-dev        — start Vite dev server (proxies /api → Go on :8080)
make run            — web-build + build + start server on :8080
make start          — concurrent dev: Go + Vite dev server
make dev            — run with air (hot reload)
make test           — go test ./... -race -count=1
make lint           — golangci-lint run ./...
make tidy           — go mod tidy + verify
make clean          — remove binary + local DB
make docker         — build Docker image
make docker-run     — run with persistent volume mounts
make migrate-status — show current SQLite migration version
make e2e-server     — build binary and start server with fresh test DB (for Playwright)
make test-e2e       — build frontend + run Playwright e2e tests
make test-e2e-ui    — open Playwright interactive UI (local dev only)
```

Dev workflow: the app is served from `:8080`. After any change, `make run` rebuilds and restarts everything. No separate Vite dev server is needed for testing.

## Testing

Test files exist across most packages. When adding new tests:

- Go: use `testing` package + `github.com/stretchr/testify` (assert/require). Add testify first: `go get github.com/stretchr/testify@latest && go mod tidy`.
- Go handler tests: use `net/http/httptest`. Create DB with a temp file — **do not use `:memory:`** because goose requires a real file path:
  ```go
  dbPath := filepath.Join(t.TempDir(), "test.db")
  db, _ := db.Open(dbPath)
  ```
- Handler auth bypass: `internal/auth/testing.go` exports `TestContext(ctx context.Context, userID string) context.Context` so test packages can inject a user ID without depending on unexported context keys.
- React: `vitest` + `@testing-library/react` + `@testing-library/user-event` + `jsdom` are configured in `web/vitest.config.ts`.
- E2E: Playwright tests live in `web/e2e/`. Use `make test-e2e` to run them; `make test-e2e-ui` opens the interactive Playwright UI.
- Run all Go tests: `make test`

## Field-Agnostic Language

The app works for any profession, not just software/IT. In all docs, UI copy, and LLM prompts:

- Do not use "engineer", "developer", "microservices", "APIs", "sprints" as generic examples
- Prefer "role", "position", "clients", "projects", "tools"
- The `HalalJobFilter` feature is an intentional product feature, not an edge case — document it plainly

## Important File Pairs (keep in sync)

- `internal/domain/types.go` ↔ `web/src/types.ts` — Go and TypeScript type definitions must match
- `internal/handler/router.go` ↔ `web/src/api/*.ts` — server routes must match frontend API calls
- `resume_markets/market_*.yaml` ↔ resume rendering logic in `internal/resume/`
