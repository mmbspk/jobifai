# Epic 154 qualification

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

Reader migration is 064_job_page_reader.sql. PR 153 now owns migrations 062 and
063 and must land before reader deployment. PR 153 test/e2e CI passed on 05c4433.
No persistent production database received the former reader migration 062. Old
isolated reader fixtures must be recreated rather than reused after renumbering.

Recovery and reuse default off. Draft PR 163 remains open. Exact-head CI, merged
prerequisite integration and the new-code read-only production-worker canary are
still gates. No merge, deployment, paid model call or live job application is
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
Fresh exact-head CI remains required for the final feature heads. PR 153 stays
open for Claude Code review at the user's request. No feature PR was merged.

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
