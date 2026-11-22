# Epic #127 validation record

Qualification is in progress in the attached worktree for PR #138. The starting
commit was d7345e3. The takeover checkpoint requires CI at its final head; the starting commit
checks do not verify these changes. No live applications have been submitted in this takeover.

## Verified so far

- Two full Go race suites passed at earlier takeover checkpoints: bot package
  771.643 and 864.862 seconds; handler package 320.441 and 332.233 seconds.
  The latest full run completed with bot 893.896 seconds and handler 324.921
  seconds. It exposed five cache fixtures that treated relevant experience as
  a general fact; those fixtures were corrected to ask total work experience.
  All affected cache/review/condition race tests then passed in 18.958 seconds.
  Final CI must verify the PR head.
- All 46 frontend test files and 378 tests passed. Frontend lint and production
  build passed. Go lint passed with zero issues after cleanup.
- Targeted real-browser control tests passed for native types, current prefilled
  values, stale question identity, duplicate labels, exact associated autocomplete,
  actual approved uploads and a country choice beyond the bounded model payload.
- Latest targeted race checks passed for current-value compatibility, native
  number bounds/steps and role-dependent answer review. Cold and partial provider
  fixtures now use the same explicit total-work-experience question.
- Question-review APIs passed owner isolation and explicit confirmation tests.
  Invalid options and injected promotion failure roll back candidate profile
  changes. Migration 058 clears legacy cached answers while preserving candidate
  profiles and applied history, and can be replayed.
- Native LinkedIn and SEEK worker fixtures both completed cold and reviewed warm
  runs through the real dispatch, single submit click, confirmation and history
  paths. Two jobs per board were recorded; the reviewed repeat used no model.
- Greenhouse handoff worker fixtures passed from LinkedIn and SEEK. Their source
  listing and verified ATS tenant/requisition identity were persisted separately.
- Eight concurrent identical interpretation misses produced one provider request.
  Changes to issuer, candidate profile or user produced separate requests.
- Attempt batch caps and cancellation, employer/field-scoped invalidation and
  rollback before submission passed targeted regressions.

## Changes tied to release criteria

| Stories | Final behavior implemented |
| --- | --- |
| #129, #133 | Shared engine wiring and dispatch for dashboard batch, direct apply and approved queue; admin modes, platform overrides, pilots, external flag and audit reason/history |
| #130, #132 | Fresh scoped controls, group/qualifier/option/constraint signatures, bounded model options with a complete local choice hash, verified fill retention, actual approved document uploads |
| #131, #137 | Current profile bindings and explicit candidate answers; independent semantic review; pending model proposals cannot activate from browser acceptance; migration invalidates old answer replay |
| #132, #133 | Dedicated LinkedIn Submit path, fresh job/employer checks, available browser quota, bounded execution and durable one-time dispatch; no confirmation means unknown and no resubmit |
| #134 | Scoped Greenhouse forms/actions, source handoff verification, strict tenant/requisition continuity, persisted external identity, normal applied-history recording and scoped positive confirmation |
| #135, #137 | Stable form-answer task and engine/attempt/platform attribution; scope-aware generation leases, attempt limits and partial field invalidation |

## Final checks and evidence still required

- A second full Go race run passed (bot 864.862 seconds; handler 332.233 seconds).
  Later scoped race checks passed for overlapping batches, worker fixtures and
  the legacy profile-JSON fix. Playwright passed all 85 app tests; Go lint is clean.
- Wrong-employer confirmation/no-resubmit and static Greenhouse success-copy
  regressions passed in the full race run. Warm currency/period compatibility
  checks passed their targeted race verification (bot 48.152 seconds; handler
  6.376 seconds).
- The matched controlled replay passed with generation caching enabled for both
  engines: Legacy base 2 requests; Adaptive cold 1, warm 0, partial 1; Legacy
  partial 1 with 2 generation-cache hits; changed-profile Adaptive 0 and Legacy 2.
  See adaptive-apply-fixture-benchmark.json for measured bytes and timings.
  Production billing, latency distributions and human review overhead remain unknown.
- Final refinements add vacancy scope for role-dependent questions, changed
  input constraints, review entries for failed local fills, and transactional
  engine setting/audit writes. Audit failure rollback passed the handler race suite.
- The existing database backup and migration to 59 completed. Restart the latest
  build with the existing data/documents and refreshed sessions.
- Operator-authorized live checks with admin@jobifai.local. Notify the operator
  before starting. Use bounded runs and inspect jobsite confirmation and Jobifai
  logs/history for each outcome; add regressions for any discovered failures.
- Commit/push PR #138 and verify CI at the exact final commit. Publish accurate
  story evidence. Keep stories and the epic open until their criteria are met.

## Known operating boundaries

Production defaults to Legacy with external follow-through disabled. Greenhouse
is the only hosted external ATS adapter. Legal/consent questions, missing facts,
ambiguous custom widgets and unmatched autocomplete suggestions pause for review.
Native reuse without a verified issuer link is restricted to the listing.
Identical and overlapping concurrent batches share pending proposals for the
same missing field and candidate/scope. They do not grant mapping verification.

The app currently uses the main checkout database and listens on 8081. Playwright
uses a separate fresh fixture database and server on 18081. The operator refreshed
both jobsite sessions and logged into the app in this chat's browser.

The running database was backed up using SQLite backup and passed integrity_check;
its pre-upgrade migration version was 53. The verified backup is at
data/backups/jobifai-pre-epic127-20261122T012140Z.db in the main checkout.
The Legacy form-answer profile encoding was corrected from base64 to a JSON
object and has a provider-facing regression.

The upgraded app is running from this chat’s managed terminal, using the main
checkout data/documents and worktree frontend. Migration 59 applied; authenticated
requests still return 200. Browser controls briefly worked and verified the
signed-in application settings, then became unavailable again. No pilot settings
were saved and no live testing has begun. Resume UI pilot setup and bounded runs
when browser access is restored; notify the operator immediately before starting.
