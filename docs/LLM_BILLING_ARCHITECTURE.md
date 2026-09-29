# LLM billing architecture

## Layers

1. **Model catalog** (`internal/pricing`) — priced models, aliases, cache/batch rates, conservative fallback for unknown IDs.
2. **Task policy** (`internal/llmpolicy`) — resolves provider/model from global config, legacy `task_models`, and optional approved admin policies.
3. **Evaluation** (`internal/eval`) — offline admin runs stored in `model_eval_runs` / `model_eval_results` (datasets live outside git).
4. **Usage ledger** (`llm_usage_events` + `internal/usage`) — append-only billing events with price snapshots.

## Cost and credits

- **Raw provider cost** is computed at call time from catalog snapshot → `raw_cost_usd_micro`.
- **Loaded cost** = raw × (1 + service markup) + per-call fee (one fee per successful logical operation).
- **Credits** = loaded USD × `CreditsPerUSD` (minimum 1 when billable).

Legacy `usage_totals` remains updated for compatibility; historical rows are not backfilled into events.

## Ollama (local)

Ollama calls are recorded in `llm_usage_events` for telemetry (task, user, model, tokens when reported, job/run IDs, latency, success). Provider raw cost is **zero**, credits burned are **zero**, and pricing source is `local/ollama`. Ollama is intended for local/developer use; paid cloud providers carry subscription quota.

## Operator workflow: evaluate a new model

1. Refresh/discover model in catalog (builtin or optional external source).
2. Mark candidate in admin model policy (not auto-promoted).
3. Run Jobifai eval dataset for the task (`model_eval_runs`).
4. Inspect failed cases and deterministic validators.
5. Compare incumbent vs challenger cost/quality.
6. Approve policy row (`state=approved`) or rollback via audit JSON.
7. Monitor economics admin dashboards.

## Privacy

Events store task, models, tokens, costs, and IDs only — never prompts, resumes, or answers.

## Environment

- Optional future: `LITELLM_ENABLED`, `LITELLM_BASE_URL`, secret `litellm_virtual_key` in secrets store (disabled by default).
