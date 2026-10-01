# Stripe Test Mode E2E log

Signed webhooks use standard `webhook.ConstructEvent` (stripe-go v86; `stripe.APIVersion=2026-08-26.dahlia`).

| Date | Scenario | HTTP | Ledger | Notes |
|------|----------|------|--------|-------|
| 2026-10-01 | `customer.subscription.updated` (mapped sub) | **200** | processed | No `IgnoreAPIVersionMismatch` |
| 2026-10-01 | `invoice.payment_failed` (CLI trigger) | **200** | processed | |
| 2026-10-01 | `invoice.paid` (CLI trigger) | **200** | processed | |
| 2026-10-01 | `checkout.session.completed` (CLI trigger) | 400 | failed | Expected: fixture lacks `jobifai_user_id` (signature OK) |
| 2026-10-01 | top-up (hosted checkout) | — | — | Use mapped user + pay session in Dashboard/CLI (same as prior E2E) |

Evidence: `data/stripe-listen.log` lines with `<-- [200] POST .../api/billing/webhook`.
