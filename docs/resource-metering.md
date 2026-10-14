# Resource metering

Resource metering lets operators charge credits for non-AI infrastructure consumed by the
platform — specifically: active browser automation sessions and PDF document rendering.
Both resource types draw from the **same shared credit balance** used for AI calls. There
is no separate resource wallet.

## Overview

| Resource type | Unit billed | Table |
|---|---|---|
| Browser automation | Per billing-increment (default 30 s of active session) | `resource_usage_events` |
| Document rendering | Per new PDF render (cache hits are free) | `resource_usage_events` |

Credits are burned atomically at the time of the event. Each event row is
idempotency-key protected — duplicate calls are silently ignored.

## Economics formula

All resource types share a single pricing pipeline:

```
raw_cost_usd_micro
  × (1 + ServiceMarkup)             → loaded_cost_usd_micro
  × CreditsPerUSD / 1_000_000       → credits_burned
```

- `raw_cost_usd_micro`: the raw infrastructure cost in micro-USD
  (1 USD = 1 000 000 µ-USD). Set per resource type in Admin → Defaults → Resource metering.
- `ServiceMarkup`: from Admin → Defaults → Quota defaults. Shared with AI billing.
- `CreditsPerUSD`: from Admin → Defaults → Quota defaults. Shared with AI billing.
- `PerCallFeeUSD` (Quota defaults) is **NOT** applied to resource billing — it is
  an AI-call-specific overhead.

### Browser automation

Raw cost per hour is converted to a per-increment raw cost using integer arithmetic:

```
raw_per_increment = raw_usd_micro_per_hour × billing_increment_secs / 3600
```

Integer truncation is intentional — it avoids sub-increment accumulation errors.

Example: `raw = 20 000 µ-USD/hr`, increment = 30 s  
→ per-increment raw = 20 000 × 30 / 3600 = **166 µ-USD**  
→ at `ServiceMarkup = 0.5`, `CreditsPerUSD = 1000`:  
  loaded = 166 × 1.5 = 249 µ-USD → credits = 249 × 1000 / 1 000 000 ≈ **0 credits**
  (integer truncation — raw must be large enough that `loaded × CreditsPerUSD ≥ 1 000 000`)

Tuning: increase `raw_usd_micro_per_hour` or `billing_increment_secs` until at least one
credit is produced per increment.

### Document rendering

```
credits_per_render = raw_usd_micro_per_render × (1 + ServiceMarkup) × CreditsPerUSD / 1_000_000
```

Integer truncation applies. Zero means no credits are burned (rendering is free at current
rates).

## Configuration

All settings live under Admin → Defaults → Resource metering.

| Field | Description |
|---|---|
| Master enable | Global gate. When off, no resource credits are ever burned. |
| Browser automation — enabled | Per-type gate for browser billing. |
| Browser automation — raw cost/hour (USD) | Infrastructure cost per browser-hour, in USD. |
| Browser automation — billing increment (seconds) | Bucket size (5–300 s). |
| Document rendering — enabled | Per-type gate for render billing. |
| Document rendering — raw cost/render (USD) | Infrastructure cost per new PDF render, in USD. |

Quota defaults (CreditsPerUSD, ServiceMarkup) also affect resource credit economics and
are configured separately under Admin → Defaults → Quota.

## Calibration guidance

The admin must choose raw cost values that match actual infrastructure spend. Steps:

1. **Measure actual spend** — run the platform under representative load for at least one
   billing period and record actual costs from your infrastructure provider.
2. **Compute raw_usd_micro_per_hour** for browsers: divide total browser session cost (USD)
   by total session-hours, then multiply by 1 000 000.
3. **Compute raw_usd_micro_per_render** for renders: divide total render cost (USD) by
   number of renders, then multiply by 1 000 000.
4. **Set ServiceMarkup** to add a margin above raw cost.
5. **Verify credit economics** — use the preview values shown on the Defaults page to
   confirm at least one credit is produced per billable unit. If either value shows 0,
   your rates are too low to produce measurable charges.
6. **Save and review** — the `/api/admin/health` response includes `resource_metering`
   with a `status` field. Target: `"ready"` or `"disabled"`.

Do NOT use vendor pricing pages (Browserless, Apify, Cloudflare, Firecrawl, or any other
provider) to set initial rates without first measuring actual costs in your own
infrastructure. Provider pricing changes; rates set from a price list rather than measured
spend will drift from reality.

## Readiness states

`GET /api/admin/health` includes a `resource_metering` object:

| `status` | Meaning | `ready` |
|---|---|---|
| `unreviewed` | Config never saved; built-in safe defaults (metering off) in use | false |
| `disabled` | Admin explicitly saved config with master toggle off | true |
| `ready` | Metering enabled; all active resource types produce ≥ 1 credit | true |
| `invalid` | Metering enabled but at least one enabled resource type produces 0 credits | false |

The `warnings` array in the response contains human-readable explanations for non-ready
states.

### Fail-closed guarantees

- `defaults_persisted = false` → metering is **always disabled** until admin reviews.
- Storage errors → `ready = false` always (fail closed, never assume ready).

## Deployment checklist

Before going to production with resource metering enabled:

- [ ] Measure actual infrastructure spend (browser sessions + render jobs).
- [ ] Set `raw_usd_micro_per_hour` and `raw_usd_micro_per_render` from measured data.
- [ ] Set `ServiceMarkup` to desired margin.
- [ ] Verify credit economics via the Defaults page preview values.
- [ ] Save the configuration — `defaults_persisted` becomes `true`.
- [ ] Confirm `GET /api/admin/health` returns `resource_metering.status = "ready"` or
  `"disabled"` (intentional).

## Security constraints

- `metadata_json` must never contain: URLs, form answers, document contents, cookies,
  credentials, or page text.
- Logs must never contain: API keys, cookies, passwords, resume/document body,
  job application answers, or raw browser/session contents.
- Raw cost, markup, and infrastructure vendor pricing are internal. They must not be
  exposed to end users.

## Related documents

- [Stripe billing lifecycle](billing.md) — customer subscriptions, webhooks, top-ups.
- [LLM billing architecture](LLM_BILLING_ARCHITECTURE.md) — AI credit economics.
