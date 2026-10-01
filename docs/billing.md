# Stripe billing lifecycle (Jobifai)

This document describes how Stripe webhooks affect credits and plan entitlement. Price IDs map to plans via admin **Credits & billing** defaults (`stripe_price_starter`, `stripe_price_pro`).

## Checkout (user-initiated)

| Flow | Route | Stripe mode | Metadata |
|------|-------|-------------|----------|
| Subscribe Starter/Pro | `POST /api/billing/checkout` | `subscription` | `jobifai_user_id`, plan implied by server-side price ID |
| Top-up pack | `POST /api/billing/topup` | `payment` | `jobifai_user_id`, `jobifai_topup=1`, `jobifai_credits` (server-chosen pack) |
| Portal | `POST /api/billing/portal` | Billing Portal | — |

**Server gates (before Stripe Checkout):**

- **Top-up:** only Starter/Pro with Stripe status `active` or `trialing` (not `past_due`, trial, expired, etc.).
- **New subscription:** rejected when a non-terminal subscription already exists locally (`active`, `trialing`, `past_due`, etc.) — use Billing Portal instead.

Customers are created/reused via `stripe_customer_id` on `user_quota`.

## Webhook events and side effects

| Event | Entitlement effect |
|-------|-------------------|
| `checkout.session.completed` | **Subscription mode:** no direct grant (subscription events apply plan). **Payment mode:** if `payment_status=paid` and top-up metadata, grant credits **once**; unpaid → **ignored** (no side effects). |
| `customer.subscription.created` | Map price → Starter/Pro; set billing period; grant monthly allowance on **new** `current_period_start` only. |
| `customer.subscription.updated` | Update status, cancel-at-period-end, price/plan; **reset usage/top-ups only** when `current_period_start` advances. Same period → metadata/plan update only. |
| `customer.subscription.deleted` | Terminate paid plan → local `expired`; clear subscription id; no new monthly allowance. |
| `invoice.paid` | Fetch current Stripe subscription; apply same logic as subscription webhooks (recovery from `past_due`, delayed renewals). |
| `invoice.payment_failed` | Fetch current Stripe subscription; apply authoritative status (no guessed `past_due` from invoice alone). API failure → webhook **failed**, retry. |

Unknown Stripe price IDs **fail closed** (no plan change; webhook row marked failed).

## Idempotency

- Every delivery is recorded in `stripe_webhook_events` keyed by Stripe `event.id`.
- Claim uses a unique insert / **processing** lock (`processing_started_at`, ~5 minute lease) so concurrent deliveries cannot double-apply side effects; stale processing claims can be reclaimed.
- Successfully **processed** or **ignored** events return HTTP 200 on retry without re-running side effects.
- **Failed** events remain retryable (Stripe may redeliver; `attempt_count` increments).
- Subscription updates compare Stripe `event.created` to `user_quota.last_stripe_state_event_created_at` so older events cannot overwrite newer state; equal timestamp cannot revive `expired` from a non-terminal update.
- Top-ups use `stripe_credit_grants.stripe_event_id` (and checkout session id) for exactly-once grants.
- Webhook rows store `user_id`, Stripe customer/subscription/session IDs when known (admin filters).

## Invoice webhooks

- `invoice.paid` and `invoice.payment_failed` fetch the current Stripe subscription and run the same apply logic as subscription webhooks.
- If Stripe API lookup fails, the webhook is marked **failed** and returns an error for retry.

## Plan changes (same billing period)

- **Upgrade (Starter → Pro):** plan becomes Pro; **current-period allowance increases** to Pro default immediately; used credits and top-ups unchanged.
- **Downgrade (Pro → Starter):** plan label/price reflect Starter; **current-period allowance is not reduced** below what was already granted for the period (avoids false overage debt). At the next `current_period_start`, Starter allowance applies normally.

On **new** billing period: reset monthly usage, clear top-ups, apply current plan allowance (roll genuine overage into debt only when used exceeded allowance at period end).

## Termination

When paid entitlement ends (`expired`), monthly allowance and **top-up balance are cleared**; enforcement blocks usage even if credits existed locally.

## Subscription states (Stripe → UI)

- **active / trialing:** paid allowance applies.
- **past_due:** allowance remains for usage; top-ups **not** sold until payment fixed (use Manage billing).
- **cancel_at_period_end:** plan stays until `period_end`; UI shows active-until date.
- **canceled / deleted:** local plan `expired`; user must resubscribe (no new trial).

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
