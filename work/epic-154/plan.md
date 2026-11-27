# Epic 154 — adaptive job-page reading

Latest status: PRs 163, 166 and 167 are merged and the clean ee81432 build is
deployed. Worker canaries and controlled runtime replays qualified the bounded
operator pilot. The final caller tests and evidence are tracked in
[production qualification](production-qualification-2026-11-26.md). The entries
below preserve earlier implementation checkpoints.

Current prerequisite: PR 153 is merged on main at e8d2d8b and its reviewed source
is already integrated into the reader branch. PR 163 implements this epic and
remains open for review. The latest checkpoint below supersedes older status
and migration notes retained in this work log.

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

Settle core DTO/API contracts before parallel contract/recovery, model/budget and recipe/admin implementation. Root assembles caller integration and qualification. Main's background-task storage uses 061; merged PR 153 uses 062/063. Reader storage uses 064, preserving deployed history. No automatic merge or deployment is part of implementation.

## Evidence

Retain sanitized sibling/lazy/display:contents, expansion replacement, sticky/duplicate header, conflicting ID, recommendation, auth/expired/absent, hostile proposal, concurrency/restart, persistence, rollback and cancellation fixtures. Measure real baseline/cold/warm/drift requests, latency, tokens/cost and browser time. Browser success in another session is not historical production-worker proof.

## Checkpoint — 2026-11-25

Draft PR #163: https://github.com/mmbspk/jobifai/pull/163, head 706f733.
Core implementation is committed/pushed. Final caller fixes/tests may remain as
uncommitted changes in manager.go and job_page_test.go; preserve and verify them.
Reader/model race suites and Chromium replay passed. Frontend 396 tests, build
and lint passed. Full backend regression run remains in progress in
/private/tmp/epic154-integration-go.log. Final admin E2E/lint need a rerun after
the integration reviewer finishes its changes; prior runs raced mid-edit.
The mobile layout assertion now waits for the existing padding transition.

Pending qualification: finish active production-hook fixtures, review final
caller changes, run final checks, commit/push fixes, and check exact-head CI.
Release canary remains pending because production does not run the new reader
yet. Merge prerequisite PR153/migration061 before deploying this migration063.
Keep recovery/reuse off until pilot qualification. Do not merge/deploy or declare
Epic154 complete automatically. No live application or paid model calls were
performed in this implementation phase.

User is considering pausing to preserve today's Codex quota; wait for their
pause/continue choice before starting more qualification work.

## Paused at user's request

User explicitly paused work and will trigger resumption tomorrow. Do not resume
automatically or create reminders. Draft PR163 remains at 706f733; subsequent
caller fixtures/fixes and lint fixes are preserved uncommitted in this worktree.
No deployment, merge, live applications or paid Jobifai model calls were done.

Latest verification: frontend build + all396 unit tests + lint pass; full app
E2E86/86 pass; Go lint0 issues. Full earlier backend run completed: handler and
server passed; bot failed TestEvaluationFailuresContinueUntilThreeDistinctJobs
and TestEvaluationSuccessResetsFailureStreakIncludingLowScore. Investigate whether
new jobpage.ErrorCode context/deadline classification is leaking into ordinary
scoring failures in listingFailureMessage, which must retain their previous
evaluation_failed handling. DB version assertion failure is from the older test
snapshot; its newer isolated DB suite passed after assertion correction.

At resume inspect /private/tmp/epic154-page-final.log for the last page suite's
result, finish the remaining regressions, run required final checks, then commit
and push all remaining changes to PR163 and qualify exact-head CI. Production
worker canary is still pending and requires new code running after PR153's
migration061 prerequisite. Do not claim Epic154 complete yet.

## Resumed — 26 November 2026

User explicitly resumed the task. PR 153 fixes at 05c4433 have green test/e2e CI.
Fixed scoring-vs-reading failure classification and nested company-panel leakage,
verified the preserved caller/meter/cancellation changes, and renumbered reader
storage to 063 to avoid PR 153 migration 062. Full browser suite86/86 and frontend
unit tests396 passed; frontend lint/build passed. Added live-DOM/HTTP-model/SQL
cold-warm-changed replay coverage; focused browser and boundary tests passed.
Full backend race checks and final lint continue. Qualification details are in
work/epic-154/qualification.md. Await explicit prerequisite merge approval; do not
merge/deploy automatically. Production-worker canary remains required for release.

Main subsequently added PR 162 and deployed migration 061_apply_url_tasks.sql.
Integrated main into both feature branches and reserved answer metadata at
062/063 and reader storage at064. The latest isolated combined checks passed
417 frontend tests and affected Go race suites; deployed task/fact/unknown-submit
retention passed migration upgrade fixtures. Both branch lints are clean. User
explicitly keeps PR153 open for Claude Code review. Do not merge either feature
PR or deploy without approval; final CI and production canary remain gates.

## Latest checkpoint — 26 November 2026

PR 153 has merged; no additional prerequisite merge is needed. PR 163 is open
for review, and test/e2e CI passed on 02c317e. Additional reader guards now cover
inline/passwordless auth, foreign heading-link issuers with matching numeric
IDs, and loading completeness through the real LinkedIn native adapter. The
reader race suite, affected worker checks and Go lint pass on the latest source.
See qualification.md for measurements. Commit/push these guards and qualify
the resulting CI head. Reader release approval and the production-worker
read-only canary remain outstanding; #161/#154 must stay open until it passes.
