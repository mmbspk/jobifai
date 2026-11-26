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

Reader migration is 063_job_page_reader.sql. PR 153 now owns migrations 061 and
062 and must land before reader deployment. PR 153 test/e2e CI passed on 05c4433.
No persistent production database received the former reader migration 062. Old
isolated reader fixtures must be recreated rather than reused after renumbering.

Recovery and reuse default off. Draft PR 163 remains open. Exact-head CI, merged
prerequisite integration and the new-code read-only production-worker canary are
still gates. No merge, deployment, paid model call or live job application is
claimed by the offline results.
