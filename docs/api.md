# jobifai API Reference

Base URL: `http://localhost:8080`

OpenAPI 3.1 spec served at runtime: `GET /api/openapi.yaml` (public, no auth required).  
Load it into Swagger UI, Insomnia, Postman, or any OpenAPI-compatible tool.

---

## Authentication

Most `/api/*` routes require a Bearer JWT in the `Authorization` header:

```
Authorization: Bearer <access_token>
```

Obtain a token via `POST /auth/login` or `POST /auth/register`. Refresh expired tokens with `POST /auth/refresh`.

---

## System

### GET /api/system/info

Return server feature flags. Public — no auth required.

- **Auth required**: No
- **Response**: `200` — `{ "vnc_enabled": true }`

`vnc_enabled` is `true` only when `/usr/share/novnc` exists (i.e. running in the Docker image).

---

## Auth — User Accounts

### POST /auth/register

Create a new user account.

- **Auth required**: No
- **Request body**:
  ```json
  { "username": "alice", "password": "s3cret" }
  ```
- **Responses**:
  - `201` — `{ "access_token": "...", "refresh_token": "..." }`
  - `409` — username already taken

```bash
curl -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"s3cret"}'
```

---

### POST /auth/login

Exchange credentials for JWT tokens.

- **Auth required**: No
- **Request body**: `{ "username": "alice", "password": "s3cret" }`
- **Responses**:
  - `200` — `{ "access_token": "...", "refresh_token": "..." }`
  - `401` — invalid credentials

```bash
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"s3cret"}'
```

---

### POST /auth/refresh

Exchange a refresh token for a new access token.

- **Auth required**: No
- **Request body**: `{ "refresh_token": "..." }`
- **Responses**:
  - `200` — `{ "access_token": "...", "refresh_token": "..." }`
  - `401` — invalid or expired refresh token

---

### POST /auth/logout

Invalidate the current refresh token.

- **Auth required**: No
- **Request body**: `{ "refresh_token": "..." }`
- **Responses**: `200` — `{ "message": "ok" }`

---

### GET /auth/google

Redirect to Google OAuth consent screen. Only available if `GOOGLE_CLIENT_ID` is configured.

- **Auth required**: No

---

### GET /auth/google/callback

Google OAuth2 callback; exchanges the code for tokens and redirects to the app.

- **Auth required**: No

---

## Me — Current User

### GET /api/me

Return the authenticated user's profile.

- **Auth required**: Yes
- **Response**: `200` — `{ "id": "...", "username": "alice" }`

```bash
curl http://localhost:8080/api/me \
  -H 'Authorization: Bearer <token>'
```

---

### PUT /api/me

Update the authenticated user's profile (e.g. change password).

- **Auth required**: Yes
- **Request body**: `{ "password": "newpass" }`
- **Responses**: `200` — `{ "message": "updated" }`

---

## Platform Auth — Browser Sessions

### POST /api/auth/launch-browser

Open a browser window for the specified platform so the user can log in manually.

- **Auth required**: Yes
- **Request body**: `{ "platform": "linkedin" }` (or `"seek"`)
- **Responses**:
  - `200` — `{ "session_id": "..." }`
  - `400` — invalid platform

```bash
curl -X POST http://localhost:8080/api/auth/launch-browser \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"platform":"linkedin"}'
```

---

### POST /api/auth/save-session

Capture and persist the browser cookies for the active session.

- **Auth required**: Yes
- **Request body**: `{ "session_id": "...", "platform": "linkedin" }`
- **Responses**: `200` — `{ "message": "session saved" }`

---

### GET /api/auth/{platform}/status

Check whether a saved session exists for the platform.

- **Auth required**: Yes
- **Path params**: `platform` = `linkedin` | `seek`
- **Response**:
  ```json
  {
    "platform": "linkedin",
    "has_session": true,
    "login_method": "manual",
    "created_at": "2026-05-01T10:00:00Z"
  }
  ```

---

### DELETE /api/auth/{platform}/session

Remove the saved session for the platform.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "session deleted" }`

---

## Bot — Automation Control

### POST /api/bot/start

Start the automation bot for the specified platform.

- **Auth required**: Yes
- **Request body**: `{ "platform": "linkedin" }`
- **Responses**:
  - `200` — `{ "message": "started" }`
  - `400` — already running or invalid platform

```bash
curl -X POST http://localhost:8080/api/bot/start \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"platform":"seek"}'
```

---

### POST /api/bot/stop

Stop the running bot.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "stopped" }`

---

### GET /api/bot/status

Return the current bot state.

- **Auth required**: Yes
- **Response**:
  ```json
  {
    "state": "running",
    "platform": "linkedin",
    "location": "Sydney",
    "keyword": "product manager",
    "started_at": "2026-05-01T09:00:00Z",
    "current_job": { "job_id": "...", "company": "Acme", "role": "Product Manager", "platform": "linkedin" },
    "today_count": 5,
    "daily_limit": 40
  }
  ```
  `state` values: `idle` | `running` | `paused` | `pending_review` | `stopped` | `error`

---

### POST /api/bot/pause

Pause the running bot after the current job completes.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "pause signal sent" }` | `404` — bot not running

---

### POST /api/bot/resume

Resume a paused bot.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "resume signal sent" }` | `404` — bot not paused

---

### GET /api/bot/review/pending

List applications waiting for manual review before submission.

- **Auth required**: Yes
- **Response**: `200` — array of `PendingReview` objects:
  ```json
  [{
    "job_id": "...",
    "company": "Acme Corp",
    "role": "Product Manager",
    "location": "Sydney, NSW",
    "platform": "linkedin",
    "link": "https://...",
    "resume_path": "job_applications/...",
    "cover_letter_path": "job_applications/...",
    "suitability_score": 8,
    "suitability_reasoning": "Strong match due to ...",
    "due_date": "2026-06-01",
    "easy_apply": true,
    "created_at": "2026-05-01T10:00:00Z"
  }]
  ```

---

### POST /api/bot/review/{job_id}/approve

Approve a pending review — the bot submits the application.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "approved" }`

---

### POST /api/bot/review/{job_id}/reject

Reject a pending review — the application is discarded.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "rejected" }`

---

## Jobs — Application History

### GET /api/jobs/applied

List submitted applications.

- **Auth required**: Yes
- **Query params**: `platform`, `limit` (default 50), `offset`
- **Response**: array of `AppliedJob`:
  ```json
  [{
    "id": "...",
    "platform": "linkedin",
    "company": "Acme Corp",
    "role": "Product Manager",
    "location": "Sydney",
    "link": "https://...",
    "resume_path": "job_applications/...",
    "suitability_score": 8,
    "applied_at": "2026-05-01T10:00:00Z"
  }]
  ```

---

### DELETE /api/jobs/applied/{job_id}

Delete a record from the applied history.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "deleted" }`

---

### GET /api/jobs/skipped

List jobs the bot skipped and the reason for each.

- **Auth required**: Yes
- **Query params**: `platform`, `skip_reason`, `limit`, `offset`
- **Response**: array of `SkippedJob` including `skip_reason` and optional `halal_verdict`.

Skip reasons: `Already applied`, `Blacklisted company`, `Blacklisted title`, `Blacklisted location`, `Company re-apply limit`, `Below suitability threshold`, `Manual skip`.

---

### DELETE /api/jobs/skipped/{job_id}

Remove a record from the skipped list.

- **Auth required**: Yes

---

### GET /api/jobs/cannot-apply

List jobs that scored well but could not be submitted automatically (e.g. external ATS).

- **Auth required**: Yes
- **Query params**: `platform`, `limit`, `offset`
- **Response**: array of `SkippedJob` objects.

---

### POST /api/jobs/cannot-apply/{job_id}/requeue

Move a cannot-apply job back to the pending review queue.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "requeued" }`

---

### GET /api/jobs/top-matches

List high-scoring jobs that are pending review (scored at or above the suitability threshold but not Easy Apply).

- **Auth required**: Yes
- **Query params**: `limit`, `offset`
- **Response**: array of `PendingReview` objects.

---

### DELETE /api/jobs/pending-review/{job_id}

Remove a job from the pending review / top matches queue without applying.

- **Auth required**: Yes

---

### POST /api/jobs/pending-review/{job_id}/mark-applied

Mark a top-matches job as applied (for applications submitted manually outside the bot).

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "marked applied" }`

---

### POST /api/jobs/pending-review/{job_id}/blacklist

Add the company of this job to the company blacklist, then delete the pending review entry.

- **Auth required**: Yes
- **Responses**: `200` — `{ "message": "blacklisted" }`

---

### GET /api/jobs/stats

Return aggregated application counts.

- **Auth required**: Yes
- **Response**:
  ```json
  { "total_applied": 42, "applied_today": 5, "total_skipped": 103 }
  ```

---

### GET /api/jobs/{job_id}

Fetch a single job record by ID.

- **Auth required**: Yes
- **Responses**: `200` — job object | `404`

---

## Resume — Document Generation

All resume endpoints accept an optional uploaded resume file to override the saved profile.

### POST /api/resume/generate

Generate a base (untailored) PDF resume from the saved profile.

- **Auth required**: Yes
- **Request**: `multipart/form-data` — optional `file`, `prompt_hint`, `linkedin_url`, `github_url`, `market`
- **Response**: `200` — PDF bytes (`Content-Type: application/pdf`)

---

### POST /api/resume/generate-tailored

Generate a job-tailored PDF resume using LLM.

- **Auth required**: Yes
- **Request**: `multipart/form-data` — `job_url` or `job_description`, optional `file`, `prompt_hint`, `linkedin_url`, `github_url`, `market`, `skip_url_fetch`
- **Response**: `200` — PDF bytes

---

### POST /api/resume/generate-cover-letter

Generate a cover letter PDF for a specific role.

- **Auth required**: Yes
- **Request**: same shape as `generate-tailored`
- **Response**: `200` — PDF bytes

---

### POST /api/resume/evaluate

Score how well the saved profile matches a job.

- **Auth required**: Yes
- **Request body**:
  ```json
  { "job_url": "https://...", "job_description": "..." }
  ```
- **Response**:
  ```json
  { "score": 8, "reasoning": "Strong match because …" }
  ```

---

### POST /api/resume/check-halal

Evaluate whether a role is permissible under Islamic employment ethics.

- **Auth required**: Yes
- **Request body**:
  ```json
  { "job_url": "https://...", "job_description": "...", "skip_url_fetch": false }
  ```
- **Response**:
  ```json
  {
    "verdict": "HALAL",
    "confidence": "HIGH",
    "summary": "This role involves …",
    "reasons": ["No interest-based activity", "…"],
    "caveats": null,
    "scholar_note": null
  }
  ```
  `verdict`: `HALAL` | `HARAM` | `DOUBTFUL`

---

### POST /api/resume/answer-questions

Use the saved profile to answer a list of application or interview questions.

- **Auth required**: Yes
- **Request body**:
  ```json
  {
    "job_url": "https://...",
    "job_description": "...",
    "questions": ["Why do you want to work here?", "Describe a challenging project."]
  }
  ```
- **Response**: array of `{ "question": "...", "answer": "..." }`

---

## Settings

### GET /api/settings/resume

Fetch the saved resume profile.

- **Auth required**: Yes
- **Response**: `ResumeProfile` JSON object (see `internal/domain/types.go`).

---

### POST /api/settings/resume

Save the resume profile.

- **Auth required**: Yes
- **Request body**: `ResumeProfile` JSON

---

### POST /api/settings/resume/upload

Upload a resume file (PDF/DOCX/TXT); extract structured profile data with LLM.

- **Auth required**: Yes
- **Request**: `multipart/form-data` with `file` field
- **Response**: `200` — extracted `ResumeProfile` JSON (not saved automatically — client must call `POST /api/settings/resume` to persist)

---

### GET /api/settings/resume/download

Download the saved profile as a YAML file.

- **Auth required**: Yes
- **Response**: `200` — YAML bytes (`Content-Type: application/yaml; filename=resume.yaml`)

---

### GET /api/settings/general

Fetch general settings (LLM config, browser config, behaviour tuning, filters).

- **Auth required**: Yes
- **Response**: `GeneralSettings` JSON

---

### POST /api/settings/general

Save general settings.

- **Auth required**: Yes
- **Request body**: `GeneralSettings` JSON

---

### GET /api/settings/preferences

Fetch work preferences (target roles, locations, blacklists, filters).

- **Auth required**: Yes
- **Response**: `WorkPreferences` JSON

---

### POST /api/settings/preferences

Save work preferences.

- **Auth required**: Yes
- **Request body**: `WorkPreferences` JSON

---

### GET /api/settings/secrets

Return which secrets are set (values are never returned in plaintext).

- **Auth required**: Yes
- **Response**:
  ```json
  {
    "llm_api_key": "•••",
    "proxy_key": "",
    "platforms": ["linkedin"]
  }
  ```

---

### POST /api/settings/secrets/api-key

Set the LLM API key or proxy key.

- **Auth required**: Yes
- **Request body**: `{ "key": "llm_api_key", "value": "sk-..." }`
- **Responses**: `200` — `{ "message": "saved" }`

---

### DELETE /api/settings/secrets/api-key

Remove a stored LLM API key or proxy key.

- **Auth required**: Yes
- **Request body**: `{ "key": "llm_api_key" }` (or `"proxy_key"`)
- **Responses**: `200` — `{ "message": "deleted" }`

---

### POST /api/settings/secrets/credentials

Store encrypted email/password credentials for a platform.

- **Auth required**: Yes
- **Request body**: `{ "platform": "linkedin", "email": "user@example.com", "password": "..." }`
- **Responses**: `200` — `{ "message": "saved" }`

---

### DELETE /api/settings/secrets/credentials

Remove stored platform credentials.

- **Auth required**: Yes
- **Request body**: `{ "platform": "linkedin" }`
- **Responses**: `200` — `{ "message": "deleted" }`

---

### GET /api/settings/styles

List available resume CSS styles.

- **Auth required**: Yes
- **Response**: `[{ "name": "default", "css_file": "default.css" }]`

---

### GET /api/settings/markets

List available resume target markets.

- **Auth required**: Yes
- **Response**: `[{ "name": "Australia", "yaml_file": "market_australia.yaml", "has_css": true }]`

---

### GET /api/settings/locations/suggest

Typeahead location suggestions for the Preferences form.

- **Auth required**: Yes
- **Query params**: `q` (search string)
- **Response**: `["Sydney, NSW", "Melbourne, VIC", …]`

---

## Files — Generated Documents

### GET /api/files/{path}

Serve generated PDF files (resumes, cover letters).

- **Auth required**: Yes
- **Path**: relative path under `job_applications/`, e.g. `api/files/2026-05-01_acme_resume.pdf`
- **Response**: PDF bytes

---

## Usage

### GET /api/usage/totals

Return cumulative LLM token usage and estimated cost since the server started, for the authenticated user.

- **Auth required**: Yes
- **Response**:
  ```json
  {
    "input_tokens": 420000,
    "output_tokens": 98000,
    "calls": 312,
    "estimated_cost_usd": 1.24
  }
  ```

---

### GET /api/usage/session

Return LLM token usage accumulated in the current server process lifetime.

- **Auth required**: Yes
- **Response**:
  ```json
  {
    "input_tokens": 124500,
    "output_tokens": 31200,
    "cache_read_tokens": 8000,
    "cache_write_tokens": 4000
  }
  ```

---

## WebSocket

### GET /ws/logs

Live log stream for the running bot session.

- **Auth required**: JWT passed as query parameter `?token=<access_token>` (auth handled inside the handler)
- **Protocol**: WebSocket, text frames containing JSON log lines:
  ```json
  { "level": "info", "message": "Evaluating Acme Corp – Product Manager", "time": "2026-05-01T10:05:00Z" }
  ```

---

## noVNC (Docker / Remote Browser)

### GET /novnc/websockify

WebSocket proxy to the Xvfb VNC server running inside the container. Used by the noVNC iframe in the Secrets settings page.

- **Auth required**: JWT passed as query parameter `?token=<access_token>`

### GET /novnc/*

Static noVNC web client assets. Available only when `/usr/share/novnc` exists (Docker image).

---

## Endpoint Implementation Status

All endpoints listed above are fully implemented. No `notImpl` (HTTP 501) responses are returned by any handler at the time of writing.
