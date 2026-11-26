# Epic 154 qualification

Latest qualification: the clean PR 167 merge (ee81432) is deployed. The actual
worker canary and the full shared-reader suite in its runtime passed qualification
with the bounded operator pilot. See [production qualification](production-qualification-2026-11-26.md)
for successful reads, the two native-only dentsu failures, actual rescan recovery,
controlled cold/warm/changed-layout measurements, rollout and remaining limits.
The entries below preserve earlier investigation checkpoints.

Historical checkpoint: PR 163 merged and its final CI passed on 05d23ca (417 frontend
tests, 92 browser tests, Go race/coverage and lint). The deployed 2dc48e8 reader
has now undergone a real read-only worker canary. It exposed production gaps;
see production-canary.md for observations, fixes and the remaining release gate.
Older entries below retain their original qualification checkpoints.

## Offline checks — 26 November 2026

- All 396 frontend unit tests passed. Frontend lint and production build passed.
- Full Playwright suite: 86/86 passed against its fresh isolated fixture backend.
- Reader/worker Chromium fixtures passed for LinkedIn and SEEK, Generate reads,
  preparation holds, prepaid browser quota, and cancellation. No Apply button
  was clicked by the reader hooks.
- Failure classification preserves the operation boundary: scoring errors remain
  evaluation_failed, including nested dependency errors; reader context, quota,
  billing, provider, identity and authentication failures retain reader codes.
- Browser fixtures preserve legitimate company introductions inside a declared
  description while excluding nested unrelated company panels and foreign jobs.
- A complete live-DOM → local HTTP provider → independent verifier → SQL recipe
  fixture passed cold, warm and changed-layout reads. Cold and changed reads each
  use two HTTP requests; warm reads a different listing's current text with zero
  HTTP requests. Reported usage and browser time are measured, not assumed.
- Full Go race suite and final Go lint are running; record final results below.

The mock provider supplies fixture token usage. These checks establish accounting
behavior, not production pricing or historic production-worker failure evidence.

## Storage and release

Reader migration is 064_job_page_reader.sql. PR 153 owns migrations 062 and
063 and is merged on main at e8d2d8b. Its reviewed source is already integrated
into this reader branch. PR 153 test/e2e CI passed on 05c4433.
No persistent production database received the former reader migration 062. Old
isolated reader fixtures must be recreated rather than reused after renumbering.

Recovery and reuse default off. PR 163 remains open for review. CI passed on
02c317e; newer reader guards require fresh exact-head CI. The new-code read-only
production-worker canary remains a release gate. No reader merge, deployment,
paid model call or live job application is
claimed by the offline results.

## Main-branch integration

Main added PR 162 after the initial reader qualification, introducing deployed
migration 061_apply_url_tasks.sql. Both feature branches now include main.
Application-answer migrations are 062/063; reader storage is 064. An isolated
combined checkout passed 417 frontend tests, frontend build/lint, and the affected
Go race suites for answers, readers, background Apply, browser compatibility,
handlers and server assembly. The deployed-061 upgrade fixture passed both with
and without the answer migrations, preserving background task results, candidate
facts and submission_unknown holds. Final lint on both feature branches is clean.

The earlier full backend run describes the pre-main-integration reader snapshot.
Fresh exact-head CI was required for those feature heads. PR 153 remained open
for Claude Code review at that checkpoint and has since merged. PR 163 has not
been merged.

The application-answer prerequisite is now integrated into the reader branch
using the same service-wiring resolutions qualified in the isolated checkout.
The earlier full Go race run passed, including bot1491.1s, handler435.1s,
jobpage80.1s and DB20.0s. It covers the pre-upstream reader snapshot; the current
combined source still requires fresh exact-head CI before release qualification.

Read-only browser startup now refuses to force-unlock an active Chrome profile,
including the race where another browser starts between preflight and launch.
A real Chromium fixture verifies both LinkedIn and SEEK reads leave the active
application browser alive, while managed reader-session reuse and prepaid quota
regressions pass. Final combined-tree lint is clean.

Authentication qualification now covers SEEK's trusted login.seek.com broker,
owned sign-in/checkpoint paths, foreign login-like URLs, tracking queries that
contain /login, and an inline login gate without listing identity. Authentication
is classified from observed trusted routes/gates; no fields or candidate nodes
from login pages reach interpretation. The complete reader race suite passed
(58.2s) after these changes; final lint reports zero issues.

## Latest reader guards — 26 November 2026

Inline sign-in gates are classified before trusted native/verifier callbacks,
including passwordless display:contents wrappers. Heading links cannot lend
another listing's identity or issuer to a candidate, even when numeric job
attributes match. Both native and adaptive paths reject owned content still
marked as loading. Native status checks exclude unrelated loading panels and
do not impose adaptive snapshot node/text caps. Complete job text beginning
with the word Loading remains readable.

Three additional Chromium regressions reproduced native loading acceptance,
nested SEEK AU/NZ issuer contamination and passwordless gate misclassification
before the fixes. Focused independent re-review found no remaining issues in
these guards. The expired native fixture now supplies its actual declared
company identity rather than borrowing an unrelated helper identity.

- Complete final reader race suite: passed, 77.827s.
- Worker/caller and evaluation-boundary race regressions: passed, 20.715s.
- Actual LinkedIn native worker loading-to-complete fixture: passed, 6.490s;
  two recorded reads, one typed incomplete outcome, then a deterministic
  current-text read, zero HTTP model requests and no Apply click.
- Final combined-tree Go lint: zero issues.

These checks cover the latest local reader source. Final pushed-head CI remains
required; CI on 02c317e does not cover these additional guards. Production-worker
qualification remains pending approved reader merge/deployment. Keep #161 and
#154 open until that read-only canary passes.

## Claude review follow-up

- Recipe replay ignores invisible duplicates before testing uniqueness. A warm
  regression keeps the recipe active and extracts current text without model calls.
- Over-limit usage sets the explicit accounting gate. Reservation and committed
  sums are checked before arithmetic can wrap; actual usage remains measurable.
- Browser callers check quota before setup, then charge only after setup succeeds.
  Failed setup across read, submission and preparation records no increment.
  The billing context remains attached to subsequent browser work; replaced meters
  are stopped before reassignment.
- Recipe upsert/readback is transactional; failed readback rolls back promotion.
  Missing recipe lookup is distinct from a persistence failure, and missing fresh
  node refs surface as staleness rather than incomplete text.
- Preserved and qualified the pending inline-authentication, foreign-heading and
  loading-state hardening. Fixtures wait for their installed body before use.

Targeted race regressions passed. Full affected race suites, final reader rerun
and exact-head CI are the remaining verification gates for this follow-up.
Production-worker canary and release approval remain pending.
