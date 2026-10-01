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
