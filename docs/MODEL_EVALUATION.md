# Model evaluation and optimization

Jobifai evaluates **task + model + effort** configurations against versioned synthetic datasets before any production policy change. Evaluation never burns customer credits or writes `llm_usage_events`.

## Architecture

- **Datasets:** `eval/datasets/synthetic/<task>/<version>/` (`manifest.json` + `cases.jsonl`). Private data stays under `data/eval/private/` (gitignored).
- **Executor:** `internal/eval/engine` loads datasets, runs candidates with bounded concurrency, applies deterministic validators, aggregates metrics, writes recommendations.
- **Observation:** `llm.BillingHooks.EvalObserver` records token/cost metadata without ledger persistence.
- **Approval:** `internal/eval/policy` requires a completed eval run, same-provider check, and billing catalog validation. Cross-provider recommendations are blocked.
- **Discovery:** `internal/pricing.LiteLLMSource` optionally refreshes public LiteLLM metadata into `model_catalog_candidates` (staging only — not billing).

## Quality gates

Validators are task-specific (`internal/eval/validators`). Critical cases (e.g. job scoring false negatives) can block recommendations via `recommend.Decide`.

## Operator workflow

1. `go run ./cmd/generate-eval-datasets` (regenerate synthetic fixtures).
2. Smoke: `go run ./cmd/eval run --task job_scoring --dataset smoke --max-cost-usd 1`
3. Review Admin → **Models** tab or `GET /api/admin/models/evals/{id}`.
4. Approve same-provider candidate: `POST /api/admin/models/policies/{task}/approve` with `eval_run_id`.
5. Rollback: `POST /api/admin/models/policies/{task}/rollback`.

Production task routing remains **task → approved policy**; no per-request auto-router.
