# Stripe Test Mode — local E2E harness

Test Mode only. Do **not** commit secrets. Put keys in `data/stripe-test-mode.local.env` (gitignored via `data/`):

```bash
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...   # from `stripe listen` output
APP_BASE_URL=http://localhost:8081
JOBIFAI_ENV=development
```

Do **not** set `JOBIFAI_STRIPE_WEBHOOK_INSECURE=1` for signed webhook verification.

## Quick start

```bash
# 1) Catalog (idempotent)
./scripts/stripe-test-mode/ensure-test-catalog.sh

# 2) Terminal A — signed webhooks
./scripts/stripe-test-mode/stripe-listen.sh

# 3) Terminal B — server (after copying whsec into local env)
source data/stripe-test-mode.local.env
./jobifai

# 4) Configure Jobifai defaults + run API checks
./scripts/stripe-test-mode/configure-jobifai-defaults.sh
./scripts/stripe-test-mode/smoke-api.sh
```

Price IDs are written to `data/stripe-test-price-ids.json` (not secret; still gitignored under `data/`).

## Webhook API version (local CLI vs stripe-go)

| Source | API version (observed) |
|--------|-------------------------|
| Stripe CLI `listen` (Test Mode, 2026-03) | `2026-08-26.dahlia` (account/CLI default) |
| `github.com/stripe/stripe-go/v82` v82.5.1 | `2025-08-27.basil` (`stripe.APIVersion`) |

`webhook.ConstructEvent` rejects events when `event.api_version` ≠ SDK version unless
`IgnoreAPIVersionMismatch` is set. The branch `fix/stripe-webhook-api-version` uses that
flag **for local signed-webhook E2E only** — not a decided production policy.

**After E2E, choose one:** pin Dashboard/CLI webhook endpoint API version to match stripe-go,
upgrade stripe-go when a release aligns with your Stripe account version, or keep explicit
mismatch handling with review.
