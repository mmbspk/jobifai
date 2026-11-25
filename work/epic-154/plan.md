# Epic 154 — adaptive job-page reading

Baseline: main e921ccb includes merged PR 152. Epic 141 is qualified in PR 153 (121892d; test/e2e CI green) and remains available for review in its own worktree.

## Delivery

1. Define bounded versioned job-page snapshots and read-only plans, separating listing ownership, description usability and application-control authority.
2. Recover loading, collapsed and lazy content with bounded owned actions and fresh scoped DOM/layout rescans.
3. Use the configured model only on genuine unresolved content; select observed refs, validate live ownership/completeness and independently verify ambiguous semantics.
4. Learn structural recipes without vacancy text/IDs or session material. Replay fresh content, invalidate failed assumptions and coalesce compatible cold misses.
5. Route LinkedIn/SEEK discovery, scoring, retries, review preparation, URL apply and Generate reads through the service.
6. Enforce and measure calls/retries, tokens/cost, wall time, browser time and persistence; add separate admin recovery/reuse rollout and retirement controls.
7. Qualify cold/warm/drift/failure behavior, exact-head CI and an authorized read-only production-worker canary.

## Boundaries

Use an independent internal/jobpage core, preserving the form engine and candidate facts. Model input excludes profiles, sessions, messaging and recommendations. Plans cannot supply invented descriptions, browser code, unrelated navigation, form fills, uploads or Submit. Private captures are optional, owned, bounded and retained separately from structural recipes. Wrong/expired/auth/unreadable pages and provider/quota/cancellation/persistence failures remain explicit and cannot fabricate a score.

## Coordination and storage

Settle core DTO/API contracts before parallel contract/recovery, model/budget and recipe/admin implementation. Root assembles caller integration and qualification. Main currently has migration 060; PR 153 adds 061. Reserve 062 for reader storage and preserve deployment order/compatible candidate history. No automatic merge or deployment is part of implementation.

## Evidence

Retain sanitized sibling/lazy/display:contents, expansion replacement, sticky/duplicate header, conflicting ID, recommendation, auth/expired/absent, hostile proposal, concurrency/restart, persistence, rollback and cancellation fixtures. Measure real baseline/cold/warm/drift requests, latency, tokens/cost and browser time. Browser success in another session is not historical production-worker proof.
