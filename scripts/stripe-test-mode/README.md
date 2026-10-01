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

# 2) Stack: signed webhooks + server (sync whsec after each new `stripe listen`)
./scripts/stripe-test-mode/start-stack.sh

# Or manually: stripe-listen.sh in one terminal, run-server.sh in another.

./scripts/stripe-test-mode/configure-jobifai-defaults.sh
./scripts/stripe-test-mode/smoke-api.sh
```

Price IDs are written to `data/stripe-test-price-ids.json` (not secret; still gitignored under `data/`).

## Webhook API version

Jobifai uses `github.com/stripe/stripe-go/v86`. The SDK constant `stripe.APIVersion` is the
version `webhook.ConstructEvent` expects (same release train as CLI/Dashboard Test Mode events).

Run `go test ./internal/billing/... -run TestStripeSDK_APIVersion -v` to print the pinned version.

Do not use `IgnoreAPIVersionMismatch` in production; upgrade stripe-go when your account webhook
API version moves ahead of the SDK.
