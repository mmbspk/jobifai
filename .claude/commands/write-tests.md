---
description: "Write missing tests for Go packages or React components. Bootstraps test frameworks if needed. Runs tests after writing to verify they pass. Use --fix to repair failing tests."
argument-hint: "[file-or-package-path | --all | --fix]"
allowed-tools: Bash(*), Glob, Grep, Read, Edit, Write
---

You are a test engineer. Your job is to write high-quality, passing tests — not generate boilerplate that looks like tests but doesn't catch bugs. Never mark the work complete while any test is red.

## Parse Arguments

- **No argument / `--all`**: discover all untested code across the project
- **A file or directory path**: focus on that specific target
- **`--fix`**: find and fix currently failing tests (run tests first, then diagnose failures)

## Phase 1 — Bootstrap Test Frameworks (run once, skip if already present)

### Go — testify
```bash
grep -q "stretchr/testify" go.mod
```
If missing:
```bash
go get github.com/stretchr/testify@latest && go mod tidy
```

### React — vitest + Testing Library
```bash
grep -q "vitest" web/package.json
```
If missing, install and scaffold:

1. Install packages:
```bash
cd web && npm install --save-dev vitest @testing-library/react @testing-library/user-event @testing-library/jest-dom jsdom
```

2. Create `web/vitest.config.ts`:
```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    globals: true,
  },
})
```

3. Create `web/src/test/setup.ts`:
```ts
import '@testing-library/jest-dom'
```

4. Add to `web/package.json` scripts:
```json
"test": "vitest run",
"test:watch": "vitest"
```

## Phase 2 — Auth Test Helper (create once if handler tests are in scope)

Handler tests in other packages need to inject a `user_id` into context, but the context key in `internal/auth/` is unexported. Create `internal/auth/testing.go` with:

```go
package auth

import "context"

// TestContext returns ctx with userID injected as the authenticated user.
// For use in tests only — do not call in production code.
func TestContext(ctx context.Context, userID string) context.Context {
    return context.WithValue(ctx, contextUserIDKey, userID)
}
```

This file is not a `_test.go` file so other packages can import it. Only create it if it doesn't already exist.

## Phase 3 — Discover Untested Code

**For the given scope**, find:
- Go files without a corresponding `<name>_test.go` in the same directory
- React components without a `<Name>.test.tsx` alongside them

Prioritise by testability tier:

**Tier 1 — Pure functions, no I/O (start here)**
- `internal/config/` — SQLite k/v store, testable with a real temp DB
- `internal/auth/password.go` — bcrypt hash/verify
- `internal/auth/uuid.go` — UUID format generation
- `internal/auth/users.go` — user store operations (uses temp DB)
- `web/src/components/StatCard.tsx`, `ScorePill.tsx`, `PlatformBadge.tsx` — pure renders

**Tier 2 — Moderate complexity (needs mocks or test infrastructure)**
- `internal/handler/*.go` — HTTP handlers via `net/http/httptest`, with real temp DB and `auth.TestContext`
- `internal/llm/client.go` — LLM HTTP client, mock the upstream with `httptest.NewServer`
- `web/src/hooks/` — React hooks needing a `QueryClientProvider` wrapper

**Tier 3 — Complex (skip browser-automation paths)**
- `internal/bot/` — test only pure helpers (blacklist checks, URL builders, deduplication). Skip anything that calls `rod.Browser` or `rod.Page` — those require integration tests with a real browser.

## Phase 4 — Write Tests

### Go Test Patterns

**Standard test file header:**
```go
package config_test // use _test suffix for black-box testing, or package config for white-box

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

**Temp DB (required — do NOT use `:memory:` because goose needs a real path):**
```go
import (
    "path/filepath"
    "testing"
    appdb "github.com/user/jobifai/internal/db"
)

func newTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    return db
}
```

**Handler test pattern:**
```go
func TestJobsApplied(t *testing.T) {
    db := newTestDB(t)
    svc := &handler.Services{DB: db, /* minimal fields */}
    router := handler.NewRouter(svc)

    req := httptest.NewRequest(http.MethodGet, "/api/jobs/applied", nil)
    req = req.WithContext(auth.TestContext(req.Context(), "user-1"))
    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
}
```

**LLM client mock:**
```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]any{/* fake response */})
}))
t.Cleanup(srv.Close)
cfg := domain.LLMConfig{Provider: "claude", Model: "test", UseProxy: true, ProxyURL: srv.URL}
client := llm.New(cfg, "fake-key")
```

**Table-driven test structure:**
```go
cases := []struct {
    name  string
    input string
    want  string
}{
    {"empty", "", ""},
    {"normal", "hello", "HELLO"},
}
for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
        got := fn(tc.input)
        assert.Equal(t, tc.want, got)
    })
}
```

### React Test Patterns

**Presentational component:**
```tsx
import { render, screen } from '@testing-library/react'
import { StatCard } from './StatCard'

test('renders value and label', () => {
  render(<StatCard label="Applied Today" value={42} />)
  expect(screen.getByText('42')).toBeInTheDocument()
  expect(screen.getByText('Applied Today')).toBeInTheDocument()
})
```

**Hook with React Query:**
```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

test('returns data', async () => {
  const { result } = renderHook(() => useMyHook(), { wrapper })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
})
```

## Phase 5 — Run and Verify After Each File

After writing each test file, run it immediately before moving to the next:

```bash
# Go — run just that package
go test ./internal/config/... -race -count=1 -v

# React — run just that file
cd web && npx vitest run src/components/StatCard.test.tsx
```

If a test fails: read the error, diagnose the root cause, fix the test or the production code, re-run. Do not continue to the next file with a red test.

## Phase 6 — Full Suite Verification

After all targeted tests pass individually:

```bash
make test
cd web && npm test
```

Report the final pass count and any remaining failures. If any test is red, fix it before finishing.

## `--fix` Mode

Run failing tests first to see what's broken:

```bash
go test ./... -race -count=1 2>&1 | grep -E "FAIL|---"
```

For each failing test:
1. Read the test file and the production file it exercises
2. Determine whether the test is wrong (testing an outdated contract) or the code is wrong (regression)
3. Fix the appropriate side
4. Re-run the specific test to confirm it's green

Never delete a failing test without understanding why it fails.
