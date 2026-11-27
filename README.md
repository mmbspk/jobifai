# jobifai

AI-powered job application automation — searches supported platforms, scores listings against your profile, fills forms, and tracks outcomes for any profession.

## Features

- Automated application submission on LinkedIn and Seek
- AI-powered profile-to-role matching with a configurable suitability score threshold
- Per-application resume and cover letter tailoring via LLM
- Optional review queue: inspect and approve tailored documents before submission when **Settings → Application → Review before submission** is enabled. Disabling it hides the approval queue and preserves existing pending applications without submitting or deleting them. Enable it again to review those applications. An automation run already waiting on those items stays waiting; update the setting to resolve them, or stop the run.
- Top Matches: track high-scoring roles that require manual application (non-Easy Apply)
- Applied / Skipped / Cannot Apply history with search and filtering
- Optional Islamic employment ethics filter (HalalJobFilter)
- Multi-provider LLM support: Claude, any OpenAI-compatible endpoint, Ollama
- LLM usage tracking per session
- Multi-user support — every user's settings, secrets, and history are fully isolated
- Browser session persistence: log in to platforms once, reuse the session across runs

## Prerequisites

- Go 1.21+
- Node.js 18+
- An LLM API key (Claude recommended; any OpenAI-compatible provider is supported)

## Getting Started

### 1. Build and run

```bash
# Build frontend + backend, then start the server on :8081
make run
```

The app is served at `http://localhost:8081`. Register an account on first launch.

### 2. Configure your LLM

Open **Settings → Secrets**, enter your API key.  
Open **Settings → General**, select your LLM provider and model.

### 3. Set up your profile

Go to **Settings → Profile**. Upload a PDF resume to auto-parse your details, or fill in the fields manually. Review and save.

### 4. Set your preferences

Go to **Settings → Preferences**. Enter your target roles, preferred locations, experience level, job types, date filters, and any company or title blacklists.

### 5. Log in to job platforms

Go to **Settings → General → Browser**, click **Launch Browser**, then log in to each platform manually inside the browser window. Return to General settings and click **Save Session**. You only need to do this once per platform.

### 6. Start the bot

Return to the **Dashboard** and press **Start**. The bot will search for roles, score them, and either apply automatically or queue them for your review, depending on your settings.

## Development

```bash
make start          # concurrent: Go server + Vite dev server (hot reload)
make dev            # Go only with air hot reload
make test           # Go tests (race detector)
make test-web       # Vitest unit tests
make test-all       # Go + frontend unit tests
make test-coverage  # Go + frontend coverage (informational; used in CI)
make test-all-e2e   # test-all + Playwright e2e

CI on pull requests: lint → test-coverage → Playwright e2e (mock LLM/OAuth/bot tests run via `make test`).
make lint           # golangci-lint + ESLint
make tidy           # go mod tidy + verify
make web-build      # build React frontend → web/dist
make build          # compile Go binary
make clean          # remove binary + local database
make migrate-status # show current SQLite migration version
make test-e2e       # build frontend + run Playwright e2e tests
make test-e2e-ui    # open Playwright interactive UI (local dev only)
```

For most changes, `make run` rebuilds both frontend and backend and restarts the server. No separate Vite dev server is required for testing.

### Comparing local and production versions

Open **Admin → System details** on each instance. **Environment** identifies local or production; **Version** identifies the release tag (when available) or commit-based build. **Git commit** links to the exact source revision. **Built (UTC)** records when the binary was built, and **Source** shows whether uncommitted changes were included. The same code can run locally and in production with the same version; a `-dirty` suffix identifies local modifications. **DB migration** identifies the schema separately.

`make build`, `make run`, `make start`, `make dev`, `make docker`, and the GCP deploy script all stamp these fields through `scripts/build.sh`. Rebuild and restart to update the displayed version. Use `./jobifai -version` to inspect a binary without starting the server. Plain `go build` can recover its compiled Git revision, but `go run` without the wrapper may be unversioned.

For a named release, build from a Git release tag or set `JOBIFAI_BUILD_VERSION=v1.2.3` for the build command. An ad-hoc `docker build` must receive `BUILD_VERSION`, `BUILD_GIT_SHA`, `BUILD_TIMESTAMP`, and `BUILD_SOURCE_STATE` build arguments; `make docker` supplies them automatically. These values are embedded in the image, not read from the checkout or environment when the server starts.

## Docker

```bash
# Build the image
make docker

# Run with persistent data volumes
make docker-run
```

`make docker-run` mounts three local directories into the container:

| Local path          | Container path            | Purpose                          |
|---------------------|---------------------------|----------------------------------|
| `./data`            | `/app/data`               | SQLite database                  |
| `./job_applications`| `/app/job_applications`   | Generated resume and cover letter PDFs |
| `./resume_style`    | `/app/resume_style`       | Custom resume CSS overrides (`style_<name>.css` — applied by the PDF renderer; market-bundled styles in `resume_markets/styles/` are what appear in Settings → Style picker) |

**Bundled in the image (not a volume):** `resume_markets/` — YAML market presets for **Default market** and resume generation. If you run the Go binary outside Docker, keep a `resume_markets/` directory next to the executable (same layout as the repo).

The container runs Chromium on a virtual display (Xvfb) and exposes a noVNC viewer embedded on **Settings → Platforms** when you use **Connect browser**. Do not run the bare `jobifai` binary on a headless Linux VPS without Xvfb — Chrome will fail with `Missing X server or $DISPLAY`.

Browser launches prefer `ROD_BROWSER_BIN`, then Rod's explicit browser setting, then installed Chrome/Chromium. Rod's bundled browser is used only when no installed browser is found. Keep the selected browser updated. If SEEK shows “Browser Upgrade”, update Chrome locally or rebuild the Docker image with `bash scripts/build.sh docker --pull --no-cache -t jobifai .` to refresh Chromium, then recreate the container and reconnect SEEK. You can select a specific executable with `ROD_BROWSER_BIN` (for example `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome` on macOS or `/usr/bin/chromium` in Docker).

**Connect browser on production:** use this Docker image (default `ENTRYPOINT` starts Xvfb and x11vnc). After **Connect browser**, sign in in the noVNC panel on the Platforms page, then **Save session**.

## API Reference

The OpenAPI 3.1 spec is served at runtime:

```
GET /api/openapi.yaml
```

No authentication required. You can load it into any OpenAPI-compatible tool (Swagger UI, Insomnia, Postman, etc.) pointed at `http://localhost:8081/api/openapi.yaml`.

## Architecture

Go backend (chi router, SQLite, Rod browser automation) + React 19 / Vite frontend served as a SPA from the same `:8081` port.

See [CLAUDE.md](CLAUDE.md) for detailed architecture documentation, package layout, and development conventions.
