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

## Credit algorithm

All resource types share a single pricing pipeline:

```
1. raw_cost_usd_micro  (integer, from admin config per-type rate)
                     ↓  integer division for browser: rawMicroPerHour × incrementSecs / 3600
                        (truncation is intentional — sub-increment accumulation is avoided;
                         if the result is 0, no credits are charged for that increment)
2. loaded_cost_usd_micro = int64(float64(raw) / 1_000_000 × (1 + ServiceMarkup) × 1_000_000)
3. credits_burned = int64(float64(loaded) / 1_000_000 × CreditsPerUSD)
                    if credits < 1 AND loaded > 0 → credits = 1  (minimum-one guarantee)
```

`PerCallFeeUSD` (Quota defaults) is **NOT** applied to resource billing — it is an
AI-call-specific overhead.

### Browser automation

Raw cost per increment uses integer arithmetic:

```
raw_per_increment = raw_usd_micro_per_hour × billing_increment_secs / 3600
```

**Integer truncation is intentional.** If the hourly rate is too low, the per-increment
raw can truncate to 0, which produces `loaded = 0` and `credits = 0` (the minimum-one
guarantee only applies when `loaded > 0`). In that case the admin UI shows
`credits_per_minute = 0` and the health endpoint returns `status = "invalid"`.

Example (valid rates): `raw = 20 000 µ-USD/hr`, increment = 30 s
→ per-increment raw = 20 000 × 30 / 3600 = 166 µ-USD (integer, truncated from 166.7)
→ at `ServiceMarkup = 0.5`: loaded = int64(166/1M × 1.5 × 1M) = 249 µ-USD
→ at `CreditsPerUSD = 1000`: credits = int64(249/1M × 1000) = 0 — but loaded > 0,
  so minimum-one applies → **1 credit per increment**

To produce more credits, increase `raw_usd_micro_per_hour` proportionally.
At `raw = 3 600 000 µ-USD/hr` (= $3.60/hr), increment 30 s:
→ per-increment raw = 30 000 µ-USD
→ loaded (markup=0.5) = 45 000 µ-USD → credits = int64(45000/1M × 1000) = **45 credits**

### Document rendering

```
loaded_usd_micro = int64(raw_usd_micro_per_render / 1_000_000 × (1 + ServiceMarkup) × 1_000_000)
credits_per_render = int64(loaded / 1_000_000 × CreditsPerUSD)
                     minimum 1 when loaded > 0
```

## What is and is not billed

### Browser automation

**Billed:**
- Every completed billing-increment bucket of active browser runtime (robot is on-page and
  executing automation steps).

**Not billed:**
- Pre-launch or post-close time (only active runtime counts).
- Paused increments where the bot is waiting on a timer or back-off delay.
- Time in `auth` / `user_action_required` state (waiting for the operator to supply cookies
  or resolve a CAPTCHA).
- Human review waits where the operator must approve or reject a job before the bot proceeds.
- Already-finalized buckets: once a bucket is written to `resource_usage_events` it is
  idempotent — re-delivery of the same key is a no-op.

**Quota exhaustion:** when the user's balance is insufficient at the start of an increment,
the bot stops further billable (and any further destructive) work for that session.

### Document rendering

**Billed:**
- New successful PDF renders where a fresh render was required (no existing artifact matched).

**Free:**
- Fetching an existing PDF artifact (download of cached render).
- Reuse of a previously rendered version when the content has not changed.
- PDF reconstruction by the Jobifai retention/eviction process (platform-internal rebuild of
  an evicted artifact; no new user-visible render has been requested).
- Failed renders (render job errors, timeouts, etc. do not produce a charge).

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
   the integer-division truncation eliminated the per-increment raw cost entirely — increase
   the hourly rate or the billing increment until the per-increment raw is ≥ 1 µ-USD.
6. **Save and review** — the `/api/admin/health` response includes `resource_metering`
   with a `status` field. Target: `"ready"` or `"disabled"`.

Do NOT use vendor pricing pages (Browserless, Apify, Cloudflare, Firecrawl, or any other
provider) to set initial rates without first measuring actual costs in your own
infrastructure. Provider pricing changes; rates set from a price list rather than measured
spend will drift from reality.

## Readiness states

`GET /api/admin/health` always includes a `resource_metering` object (present even when
the resource service is unavailable — it fails closed):

| `status` | Meaning | `ready` |
|---|---|---|
| `unreviewed` | Config never saved; built-in safe defaults (metering off) in use | false |
| `disabled` | Admin explicitly saved config with master toggle off | **true** |
| `ready` | Metering enabled; all active resource types produce ≥ 1 credit | true |
| `invalid` | Metering enabled but at least one enabled resource type produces 0 credits | false |

The `warnings` array in the response contains human-readable explanations for non-ready
states.

### Fail-closed guarantees

- `defaults_persisted = false` → metering is **always disabled** until admin reviews.
- Storage errors → `ready = false` always (fail closed, never assume ready).
- Service unavailable → `ready = false`, `status = "invalid"`, full structural response returned.

## Deployment checklist

Before going to production with resource metering enabled:

- [ ] Measure actual infrastructure spend (browser sessions + render jobs).
- [ ] Set `raw_usd_micro_per_hour` and `raw_usd_micro_per_render` from measured data.
- [ ] Set `ServiceMarkup` to desired margin.
- [ ] Verify credit economics via the Defaults page preview values (must show ≥ 1 credit).
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
