# Stripe billing lifecycle (Jobifai)

This document describes how Stripe webhooks affect credits and plan entitlement. Price IDs map to plans via admin **Credits & billing** defaults (`stripe_price_starter`, `stripe_price_pro`).

## Checkout (user-initiated)

| Flow | Route | Stripe mode | Metadata |
|------|-------|-------------|----------|
| Subscribe Starter/Pro | `POST /api/billing/checkout` | `subscription` | `jobifai_user_id`, plan implied by server-side price ID |
| Top-up pack | `POST /api/billing/topup` | `payment` | `jobifai_user_id`, `jobifai_topup=1`, `jobifai_credits` (server-chosen pack) |
| Portal | `POST /api/billing/portal` | Billing Portal | — |

Customers are created/reused via `stripe_customer_id` on `user_quota`.

## Webhook events and side effects

| Event | Entitlement effect |
|-------|-------------------|
| `checkout.session.completed` | **Subscription mode:** no direct grant (subscription events apply plan). **Payment mode:** if `payment_status=paid` and top-up metadata, grant credits **once** (`stripe_credit_grants` + event id). |
| `customer.subscription.created` | Map price → Starter/Pro; set billing period; grant monthly allowance on **new** `current_period_start` only. |
| `customer.subscription.updated` | Update status, cancel-at-period-end, price/plan; **reset usage/top-ups only** when `current_period_start` advances. Same period → metadata/plan allowance update only. |
| `customer.subscription.deleted` | Terminate paid plan → local `expired`; clear subscription id; no new monthly allowance. |
| `invoice.paid` | Acknowledged (renewals driven by subscription period start). |
| `invoice.payment_failed` | Set subscription status `past_due` (metadata); keep access per Stripe `active`/`past_due` rules. |

Unknown Stripe price IDs **fail closed** (no plan change; webhook row marked failed).

## Idempotency

- Every delivery is recorded in `stripe_webhook_events` keyed by Stripe `event.id`.
- Successfully **processed** or **ignored** events return HTTP 200 on retry without re-running side effects.
- Top-ups use `stripe_credit_grants.stripe_event_id` (and checkout session id) for exactly-once grants.

## Subscription states (Stripe → UI)

- **active / trialing:** paid allowance applies.
- **past_due:** allowance remains; UI shows manage billing (`block_code=past_due` informational).
- **cancel_at_period_end:** plan stays until `period_end`; UI shows active-until date.
- **canceled / deleted:** local plan `expired`; user must resubscribe (no new trial).

## Plan changes (Starter ↔ Pro)

On `subscription.updated` within the **same** billing period: update `period_allowance_micro` to the new plan default; **do not** reset `period_used_micro` or top-up balance.

On **new** billing period: reset monthly usage, clear top-ups, apply new allowance (and roll over overage into debt if applicable).

## Configuration (canonical)

| Variable | Purpose |
|----------|---------|
| `STRIPE_SECRET_KEY` | API calls (checkout, portal, reconcile, public plan prices) |
| `STRIPE_WEBHOOK_SECRET` | Required for signed webhooks in production |
| `JOBIFAI_STRIPE_WEBHOOK_INSECURE=1` | Test/dev only: accept unsigned webhook JSON |
| `APP_BASE_URL` | Checkout return URLs |

Admin billing summary reads the same helpers as runtime (`internal/billing/config.go`).

## Admin recovery

`POST /api/admin/billing/users/{user_id}/reconcile` lists the Stripe customer’s subscriptions, picks the best active-like subscription, and applies it locally— or clears paid entitlement if none exist.
