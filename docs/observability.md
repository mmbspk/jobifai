# Observability — log event contract

All structured log lines are UTF-8 JSON objects emitted by zerolog.
Console mode (`LOG_FORMAT=console`) reformats them for human readability; the
field names below apply to the raw JSON in both modes.

## Common fields (every line)

| Field | Type | Description |
|---|---|---|
| `time` | RFC3339 UTC string | Emission timestamp |
| `level` | string | `debug` / `info` / `warn` / `error` |
| `message` | string | Human-readable summary |
| `service` | string | Always `"jobifai"` |
| `env` | string | `JOBIFAI_ENV` value; defaults to `"development"` |
| `build_sha` | string | Short git SHA from `-ldflags` at build time |
| `log_id` | UUID string | Unique per line; shared by WS stream and REST seed endpoint (Story #101) |

## Run-scoped fields (bot run lines)

| Field | Type | Description |
|---|---|---|
| `user_id` | string | Owner of the run |
| `automation_run_id` | string | Identifies one `Start()` invocation; carried on all lines emitted by the bot logger |

## Lifecycle events

Each lifecycle event carries `event` and `schema_version: "1"` fields in
addition to the common fields above.  Only these `event` values are considered
stable; other log lines may change without notice.

### `run_start`

Emitted once when a run is successfully registered.

| Field | Description |
|---|---|
| `platform` | `"linkedin"` / `"seek"` |

### `run_end`

Emitted once in the goroutine defer, after the bot's `Start()` returns.

| Field | Description |
|---|---|
| `platform` | Same as `run_start` |
| `outcome` | Final `BotState` string |

### `attempt_result`

Emitted by `finishAttempt` for every attempt outcome (success, failure,
already-applied, cancelled, timed-out, blocked, skipped, submission-unknown,
interrupted).

| Field | Description |
|---|---|
| `attempt_id` | UUID; stable across retries of the same job |
| `automation_run_id` | Run that owns this attempt (empty for standalone direct-apply) |
| `user_id` | |
| `job_id` | Platform job ID |
| `url` | Job URL with query string stripped |
| `platform` | |
| `entry_point` | `runSubmit` / `applyFromURL` / `automated_linkedin` / `automated_seek` / `automated_queue` |
| `outcome` | `submitted_confirmed` / `already_applied` / `failed` / `cancelled` / `timed_out` / `blocked` / `skipped` / `submission_unknown` / `interrupted` |
| `stage` | Last processing stage when the outcome was determined |
| `failure_code` | Short kebab-case reason code (empty on success) |
| `duration_ms` | Wall time from `newAttemptContext` to `finishAttempt` |

### `user_action_required`

Emitted when an application cannot be completed automatically and requires
manual user action (e.g. LinkedIn asks for a review step).

| Field | Description |
|---|---|
| `attempt_id` | Present for `submitEasyApply` path; absent for `recordApplyFailure` path |
| `job_id` | |
| `platform` | |
| `reason` | Raw error message (may contain LinkedIn error text) |

### `quota_stop`

Emitted once when the browser billing meter reports quota exhausted and the
run stops iterating jobs.  Not emitted on manual stop or context cancellation.

| Field | Description |
|---|---|
| `automation_run_id` | |

### `llm_request_result`

Emitted after every successful LLM API call (all providers: Claude, OpenAI-compatible, Ollama).

| Field | Description |
|---|---|
| `task` | Task label from `CallContext` |
| `provider` | `"claude"` / `"openai"` / `"ollama"` |
| `requested_model` | Model string from config |
| `actual_model` | Model string returned by the API |
| `actual_model_verified` | `true` when the API confirmed the actual model |
| `input_tokens` | Prompt tokens billed |
| `output_tokens` | Completion tokens billed |
| `duration_ms` | Network + decode time |
| `success` | Always `true` for this event (failures use `level: error`) |
| `job_id` | From `CallContext` (empty when not set) |
| `run_id` | `automation_run_id` from `CallContext` |

### `document_render`

Emitted after a resume or cover-letter PDF is produced by `htmlToPDF`.

| Field | Description |
|---|---|
| `size_bytes` | Byte length of the rendered PDF |
| `duration_ms` | Time from Chrome launch to PDF read |

## Sensitive-data handling

- **URL query strings**: stripped from all `url` fields in lifecycle events via `redactURL()`. Raw URLs may still appear in non-lifecycle debug/info logs.
- **Secrets**: API keys, JWT tokens, and passwords must never appear in log output. The LLM client logs provider and model but not request/response bodies.
- **Error messages**: error text from external services may appear in `reason` / `failure_code` fields. Avoid logging raw HTML, JavaScript, or form-field contents.

## Log format configuration

| Variable | Values | Default |
|---|---|---|
| `LOG_FORMAT` | `json` \| `console` | `json` when `JOBIFAI_ENV=production`, else `console` |
| `JOBIFAI_ENV` | any string | `development` |

The `service`, `env`, and `build_sha` base fields are stamped on all lines
before the database opens, so startup failures are structured.
