---
description: "Write or update project documentation: CLAUDE.md, README.md, API reference, and user guide. Targets: claude-md | readme | api-docs | user-guide | --all"
argument-hint: "[claude-md | readme | api-docs | user-guide | --all]"
allowed-tools: Bash(*), Glob, Grep, Read, Edit, Write
---

You are a technical writer. Your job is to produce clear, accurate documentation that reflects the current state of the codebase. Always read the relevant source files first — never invent details. Keep all language field-agnostic (the app works for any profession, not just software/IT).

## Parse Arguments

- **No argument / `--all`**: write or update all four documentation artifacts
- **`claude-md`**: update `CLAUDE.md` only
- **`readme`**: write `README.md`
- **`api-docs`**: generate `docs/api.md`
- **`user-guide`**: generate `docs/user-guide.md`

## Before Writing Anything — Read These Files

Always read these before any documentation target:

- `CLAUDE.md` (existing developer context)
- `internal/handler/router.go` (complete route map)
- `internal/domain/types.go` (data model)
- `web/src/App.tsx` or router file (frontend routes)
- `Makefile` (all available commands)
- `Dockerfile` (deployment context)
- `README.md` (existing content to preserve or replace)

The OpenAPI spec lives at `api/openapi.yaml` and is served at runtime as `GET /api/openapi.yaml` (public, no auth). Reference this URL in any documentation that mentions the API.

---

## Target: `claude-md` — Update CLAUDE.md

Read the existing `CLAUDE.md`. Then scan for changes since it was written:

1. **New packages**: `ls internal/` — are any packages missing from the Package Layout section?
2. **New Makefile targets**: compare `Makefile` targets to the Commands section
3. **New interfaces**: check `internal/handler/handler.go` for interfaces not yet documented
4. **Changed patterns**: any new architectural decisions visible in recent commits?

Update `CLAUDE.md` in-place. Do not remove accurate existing content. Preserve the field-agnostic language requirement section exactly.

---

## Target: `readme` — Write README.md

Replace the current `README.md` with a complete document. Read `Dockerfile`, `Makefile`, `internal/handler/router.go`, and the web frontend entry point to fill in accurate details.

Structure:

```markdown
# jobifai

[One sentence: what the tool does, for whom, without assuming their profession]

## Features

- Automated application submission on supported platforms
- AI-powered profile-to-role matching with configurable score threshold
- Per-application resume and cover letter tailoring via LLM
- Review queue: inspect and approve before any application is submitted
- Top Matches: track high-scoring roles that require manual application
- Applied / Skipped / Cannot Apply history with search and filtering
- Optional Islamic employment ethics filter (HalalJobFilter)
- Multi-provider LLM support (Claude, OpenAI-compatible, Ollama)
- LLM usage tracking per session
- Multi-user support with isolated settings and secrets

## Prerequisites

- Go 1.21+
- Node.js 18+
- An LLM API key (Claude recommended; OpenAI-compatible providers supported)

## Getting Started

### 1. Build and run

[exact commands from Makefile]

### 2. Configure your LLM

Open Settings → Secrets, enter your API key.
Open Settings → General, select your LLM provider and model.

### 3. Set up your profile

Upload your resume in Settings → Profile. The app will parse it automatically.
Fill in your work preferences in Settings → Preferences: target roles, locations, filters.

### 4. Log in to job platforms

Go to Settings → General → Browser, click "Launch Browser", then log in to each platform manually. Return to Settings → General and click "Save Session".

### 5. Start the bot

Return to the Dashboard and press Start.

## Development

[make start, make test, make lint, make dev with air]

## Docker

[make docker, make docker-run with volume info]

## API Reference

The OpenAPI 3.1 spec is served at `GET /api/openapi.yaml` (public, no auth).
Link to it and note it can be loaded into Swagger UI, Insomnia, Postman, etc.

## Architecture

Go backend (chi router, SQLite, rod browser automation) + React 19 / Vite frontend.
See CLAUDE.md for detailed architecture documentation.
```

---

## Target: `api-docs` — Generate docs/api.md

Create the `docs/` directory if it doesn't exist. Generate `docs/api.md`.

Read `internal/handler/router.go` completely. For every route, document:

- Method + path
- Auth required: Yes / No
- Brief description (infer from handler name and domain types)
- Request body: JSON schema derived from `internal/domain/types.go` (where applicable)
- Response: HTTP status codes + JSON schema
- Example curl

Group routes by domain: **Auth**, **Me**, **Platform Auth**, **Bot**, **Jobs**, **Resume**, **Settings**, **Usage**, **WebSocket**.

For each group, note which endpoints are fully implemented vs. returning 501 Not Implemented (grep for `notImpl(` in handler files).

---

## Target: `user-guide` — Generate docs/user-guide.md

Create `docs/user-guide.md`. Read the frontend page components in `web/src/pages/` to understand what each screen shows and does. Write in plain language, no technical jargon.

Structure:

```markdown
# User Guide

## Overview

[What the tool does for the user, why it saves time, field-agnostic framing]

## Initial Setup

Step-by-step from zero to first automated application.

## Dashboard

[Bot status states and what they mean, stats, live log panel]

## Review Queue

[What "pending review" means, how to approve or reject, what happens after each action]

## Top Matches

[What appears here, how the score threshold works, why these require manual application]

## Application History

[Applied tab, Skipped tab, Cannot Apply tab — what each means and how to use them]
[Mark Applied button, Requeue button]

## Generate Documents

[The Generate page — create a tailored resume or cover letter for any role on demand]

## Settings

### General
[LLM provider, model, browser config, behavior tuning (daily limit, pauses), review mode toggle, score threshold]

### Preferences
[Target roles, locations, experience level, job types, date filters, blacklists, apply-once-per-company]

### Profile
[Resume data fields, how to upload a PDF to auto-parse, the profile editor]

### Secrets
[API key entry, platform credentials — note that secrets are encrypted at rest]

## Troubleshooting

- Bot starts then immediately stops
- Browser connection fails
- LLM errors / empty responses
- Application stuck in pending review
- Score threshold too strict / too loose
```

---

## After Writing

Verify the documentation is consistent with the code:

```bash
# Confirm every route in api.md exists in router.go
grep "POST /api/bot/start" internal/handler/router.go

# Confirm make targets in README exist in Makefile
grep "^run:" Makefile
```

Flag any discrepancy in your response rather than silently leaving it wrong.
