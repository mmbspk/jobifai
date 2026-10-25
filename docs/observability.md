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
| `log_id` | UUID string | Unique per line; stamped by `logIDHook` on the bot's scoped logger. Present on all lines emitted through the bot logger; not present on lines from the global logger (e.g. HTTP handler logs, LLM result events) |

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
| `attempt_id` | UUID; freshly generated for each attempt — not stable across separate retries of the same job |
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
| `reason_code` | Bounded reason code (e.g. `linkedin_submit_unconfirmed`, `external_apply`, `linkedin_listing_unverified`) — never raw error text |

### `quota_stop`

Emitted at most once per run when the browser billing meter reports quota
exhausted and the run stops iterating jobs. Not emitted on manual stop or
context cancellation.

| Field | Description |
|---|---|
| `automation_run_id` | |
| `failure_code` | `browser_quota_exceeded` when the jobifai credit balance is exhausted; `browser_meter_failure` for other meter errors |

### `llm_request_result`

Emitted after every LLM API call (all providers: Claude, OpenAI-compatible, Ollama) — both success and failure paths.

| Field | Description |
|---|---|
| `user_id` | From `CallContext` |
| `automation_run_id` | From `CallContext` (empty when not set) |
| `job_id` | From `CallContext` (empty when not set) |
| `task` | Task label from `CallContext` |
| `provider` | `"claude"` / `"openai"` / `"ollama"` |
| `requested_model` | Model string from config |
| `actual_model` | Model string returned by the API (omitted on failure) |
| `actual_model_verified` | `true` when the API confirmed the actual model (omitted on failure) |
| `input_tokens` | Prompt tokens billed (omitted on failure) |
| `output_tokens` | Completion tokens billed (omitted on failure) |
| `duration_ms` | Network + decode time |
| `success` | `true` on success; `false` on any error |
| `failure_code` | Bounded error code (`cancelled`, `timeout`, `rate_limited`, `auth_failed`, `provider_unavailable`, `provider_error`). Present only on failure |

### `document_render`

Emitted after `htmlToPDF` completes — both success and failure paths.  The
context must carry an LLM call context (attached via `llmCtx` in the bot, or
`WithCallContext` elsewhere) for correlation fields to be populated.

| Field | Description |
|---|---|
| `success` | `true` on success; `false` on failure |
| `doc_type` | `"resume"` or `"cover_letter"` (empty if called without `WithDocType`) |
| `user_id` | From call context |
| `automation_run_id` | From call context |
| `job_id` | From call context |
| `size_bytes` | Byte length of the rendered PDF (success only) |
| `duration_ms` | Time from Chrome launch to PDF read |

## Sensitive-data handling

- **URL query strings and credentials**: stripped from all `url` log fields via `redactURL()` (applies to all log paths, including info/warn/error). Rod navigation errors that embed the target URL are passed through `sanitizeErr()` before logging.
- **Secrets**: API keys, JWT tokens, and passwords must never appear in log output. The LLM client logs provider and model but not request/response bodies.
- **Error messages**: error text from external services may appear in `reason` / `failure_code` fields. Avoid logging raw HTML, JavaScript, or form-field contents.

## Log format configuration

| Variable | Values | Default |
|---|---|---|
| `LOG_FORMAT` | `json` \| `console` | `json` when `JOBIFAI_ENV=production`, else `console` |
| `JOBIFAI_ENV` | any string | `development` |

The `service`, `env`, and `build_sha` base fields are stamped on all lines
before the database opens, so startup failures are structured.

## Log collection

### Collector

**Production**: Grafana Alloy v1.21.2 sidecar. Reads the `jobifai` container's
stdout/stderr via the Docker socket (`loki.source.docker`). Positions are
persisted to a named volume (`alloy_data`) so Alloy resumes collection after
restart; reading position ≠ Loki acknowledgement — in-flight lines not yet
acknowledged may be lost on Alloy restart.

**Local smoke test**: `loki.source.file` tailing `/tmp/jobifai-smoke.log`; start
with `make obs-up` (see §Local smoke test below).

### Stream labels

```
{service="jobifai", env="production", level="info|warn|error|debug"}
```

`level` and `env` are promoted from the JSON body by `loki.process`. High-cardinality
IDs (`user_id`, `automation_run_id`, `log_id`, `job_id`) stay in the JSON body and
are queried via `| json | field="value"` in LogQL.

### Durability

Delivery is **best-effort**. Alloy retries failed batches with exponential
backoff (500 ms → 5 min, up to 10 attempts). After retry exhaustion the batch
is dropped. Docker log rotation (max 100 MB × 5 files = 500 MB buffer) means
logs older than the buffer window are unrecoverable.

Alloy resumes from saved positions on restart; it does not guarantee delivery of
lines that were read into memory but not yet acknowledged at the time of the crash.

### Free-tier limits

Grafana Cloud Free (verified October 2026 at grafana.com/pricing):

| Limit | Value |
|---|---|
| Ingestion | 50 GB / month |
| Retention | 14 days |

Pushes beyond the ingestion limit return 429. No automatic paid upgrade.

### Retry configuration

```
min_backoff_period  = 500ms
max_backoff_period  = 5m
max_backoff_retries = 10
```

After 10 failed attempts the in-flight batch is dropped. Alloy's UI
(`http://127.0.0.1:12345` via SSH tunnel — see runbook) shows a graph of
delivery attempts and drops.

### Required environment variables (on VM in `/opt/jobifai/.env`)

| Variable | Description |
|---|---|
| `LOG_FORMAT` | Must be `json` for structured collection |
| `JOBIFAI_ENV` | Set to `production`; stamped on all log lines |
| `GRAFANA_LOKI_URL` | Full push URL ending in `/loki/api/v1/push` |
| `GRAFANA_LOKI_USER` | Numeric tenant ID (not the org slug) |
| `GRAFANA_LOKI_API_KEY` | API key with **Logs Push** scope only |

### Local smoke test

```bash
make obs-up                                          # start Loki + Grafana + Alloy
LOG_FORMAT=json JOBIFAI_ENV=local-smoke make run 2>&1 | tee /tmp/jobifai-smoke.log
# Explore: http://127.0.0.1:3000 → Explore → {service="jobifai"}
# Alloy UI: http://127.0.0.1:12345
make obs-down                                        # stop stack and remove containers
```

Alloy in the local stack reads `/tmp/jobifai-smoke.log` via a bind-mount of the host
`/tmp` directory. `loki.source.docker` cannot see a host-process binary; the
file-source variant is intentional.

### Further setup and runbook

See [docs/runbooks/observability-setup.md](runbooks/observability-setup.md) for:
- Grafana Cloud credentials setup
- Production deployment order and pre-flight checks
- Readiness verification and Alloy UI access
- LogQL queries for lifecycle events
- Recovery tests (positions file + connectivity interruption)
- Troubleshooting guide
