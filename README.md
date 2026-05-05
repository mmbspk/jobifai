# jobifai

AI-powered job application automation — searches supported platforms, scores listings against your profile, fills forms, and tracks outcomes for any profession.

## Features

- Automated application submission on LinkedIn and Seek
- AI-powered profile-to-role matching with a configurable suitability score threshold
- Per-application resume and cover letter tailoring via LLM
- Review queue: inspect and approve tailored documents before any application is submitted
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
# Build frontend + backend, then start the server on :8080
make run
```

The app is served at `http://localhost:8080`. Register an account on first launch.

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
make test           # go test ./... -race -count=1
make lint           # golangci-lint run ./...
make tidy           # go mod tidy + verify
make web-build      # build React frontend → web/dist
make build          # compile Go binary
make clean          # remove binary + local database
make migrate-status # show current SQLite migration version
```

For most changes, `make run` rebuilds both frontend and backend and restarts the server. No separate Vite dev server is required for testing.

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
| `./resume_style`    | `/app/resume_style`       | Custom resume CSS overrides      |

The container runs Chromium in a headless virtual display (Xvfb) and exposes a noVNC viewer at `/novnc/` so you can inspect the browser remotely.

## API Reference

The OpenAPI 3.1 spec is served at runtime:

```
GET /api/openapi.yaml
```

No authentication required. You can load it into any OpenAPI-compatible tool (Swagger UI, Insomnia, Postman, etc.) pointed at `http://localhost:8080/api/openapi.yaml`.

## Architecture

Go backend (chi router, SQLite, Rod browser automation) + React 19 / Vite frontend served as a SPA from the same `:8080` port.

See [CLAUDE.md](CLAUDE.md) for detailed architecture documentation, package layout, and development conventions.
