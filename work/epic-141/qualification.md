# Epic 141 qualification

Scope: guided Profile onboarding, background confirmation and preserved PR #138 mappings. No production deployment or new live job applications were performed for this Epic. All application/browser/provider qualification used isolated fixtures.

## Behavior covered

- Five core topics by default; five optional topics and custom answers remain collapsed. Additional answers support search and pages of 20, preserving hidden edits and employer/job scope. Imported clear facts are editable before an explicit combined profile-and-answers save. Initial profile loading, tab switching, refetches, concurrent saves, pending saves and invalid pay drafts preserve edits.
- Legacy values, false/null, country/pay qualifiers, custom scopes and learned cache rows survive migration 062 and partial saves. Current/future sponsorship remains separate. Explicit null clears supplied date/text topics without erasing other facts. Metadata does not affect factual hashes or warm reuse.
- Clear quoted CV facts prefill drafts through the existing single extraction call; ambiguous, stale and absent facts do not become assertions. Owned import tokens attach minimized source evidence only on matching explicit saves; canonical YAML stays zero-call.
- Durably recorded supported descriptive composition can proceed. Missing required facts defer one application while the next eligible job continues. Provider, quota, browser, identity, profile-read and persistence failures remain operational. Fact deferrals preserve the operational retry count.
- Repeated/dismissed questions stay quiet when facts and proposals are unchanged. Warm derived reuse records separate immutable attempt-use history with zero additional provider requests.
- Changed declared character limits trigger a fresh proposal plus independent verification. Original filled text remains replaced; only final text inherits a confirmed submission. Retried encounter IDs cannot change control/job identity. Duplicate controls without IDs preserve both submitted uses, and single-field repair rescans retain the browser ordinal.
- Corrections are owned, atomic and versioned. They preserve past submitted/unknown evidence and schedule no applications. Only safely blocked fact deferrals become eligible for normal future evaluation. Active/unknown/confirmed attempts and pending/approved queues remain held.
- Preparation and recovery use current DOM identity and the shared answer plan. Vision predictions and search filters do not establish candidate facts or grant browser authority.
- A database containing 2,000 learned mappings produces the same ten-topic catalog and no automatic candidate questions; 55 required review groups paginate as three bounded pages without duplication or cache loss.

## Validation evidence

- Frontend: 405 unit tests passed; TypeScript/Vite production build and ESLint passed.
- Browser: all 91 tests passed against the isolated backend. All five isolated Application answers fixtures also passed, including 320px imported onboarding, keyboard tabs, grouped notifications, preserved drafts, malformed pay and paginated additional-answer edits. Screenshots were inspected visually.
- Storage/domain/migrations: race suites passed. Final application-answer service race suite includes the 2,000-mapping and immutable-identity regressions.
- CV extraction: complete race suite passed using local provider fixtures.
- Runtime: focused race suites passed for current-fact guards, later-review use, warm reuse, dismissed repeats, retry accounting, preparation/recovery and changed-limit history.
- Go lint: zero issues. Full Go race suite and PR CI status are reported on the PR rather than inferred from earlier runs.

## Limits and rollback

Fixture success does not establish that every future jobsite form works or validate the separate production LinkedIn description-reading issue. That investigation remains in another chat. Native identity, approved-document, one-time Submit and unknown-outcome safeguards stay active.

For UI rollback, retain the compatible backend/schema and candidate/history records. An older backend serializer can omit newly introduced fields on save; protect edits or preserve that serialization support before rolling it back. Do not use a destructive schema downgrade to undo the UI.

### Claude review follow-up

- Added regression coverage for topic-bearing confirmations, affected-review
  invalidation, unrelated sequential confirmation, and immutable encounter revisions.
- Resolved reviews stay quiet only for the confirmed scoped answer at the saved
  factual revision. Changed proposals/facts reopen and notify; actual-use history remains.
- Added malformed/null CV pointer defenses, present-tense rights hydration,
  compensation-specific conflict detection, precompiled jurisdiction/claim matching,
  and short-country alias boundary regressions.
- Input errors are distinct from internal errors; encounter ID collision handling
  is user scoped and rolls back without creating another user's review.
- UI conflicts retain drafts and require refresh; transient failures remain retryable.

Upstream integration on 26 November: main owns migration 061_apply_url_tasks.sql. Unreleased answer metadata/confirmation migrations are now 062/063. Both deployed task storage and candidate answer storage are retained. New-head CI must pass before merge.
