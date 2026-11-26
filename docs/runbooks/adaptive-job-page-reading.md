# Adaptive job-page reading

Epic #154 covers verified listing identity and descriptions on LinkedIn and SEEK.
The form engine, application-answer cache and submission guards retain their
existing contracts. A description-reading model cannot authorize an application.

## Resolution and ownership

The shared reader tries the platform baseline, a compatible structural recipe,
bounded local recovery, then a typed model proposal and independent verification.
Browser reads revalidate current origin, subject, visible header, fresh refs,
epoch, ownership and complete rendered content. URL matching, opaque classes,
recommendations and model confidence cannot prove ownership. Actions are limited
to waits, owned scrolling and owned expansion. Replacement nodes need fresh refs.

Known layouts use zero interpretation requests. Recipes contain structural
selectors and scope, never vacancy text, job ID, profile, cookies or node refs.
Warm reads extract the current listing's text and recheck live assumptions.
Matching cold misses are serialized within the worker; followers replay their
own listing after verified learning. Failed or canceled reads cannot learn.
Safe content without unique reusable structure is accepted without a recipe;
failed content or identity proof is rejected. Incompatible assumptions invalidate
a recipe; operator retirement is sticky against concurrent learning.

Discovery/scoring, evaluation retries, approved queue reads, review preparation,
direct URL application and Generate use the same reader. Generate opens a new
page in the account's saved worker browser for supported boards. Other sources
retain the HTTP fetcher. Failures remain unscored rather than inventing a score.

## Rollout and controls

Recovery and reuse both default to **off**. Admin → Automation → Job description
reading enables each independently for all accounts or listed pilot account IDs.
Settings use optimistic revisions and an atomic audit. Active reads recheck the
switches at action, model and acceptance boundaries. Configured caps bound nodes,
text/images, description size, actions, HTTP attempts/retries, tokens, cost and time.
The default snapshot is 12,000 characters. Larger content must fit an explicitly
raised cap; exhaustion cannot silently remove competing candidates.

Models use existing deployment/personal credentials, approved task policies,
quota and billing under job_page_reading. There is no automatic provider switch
or exact-generation cache reuse. Conservative request reservations prevent
another request when remaining bounds cannot cover it. Successful reported usage
settles reservations; uncertain usage is retained and marked unknown final cost.
Events record actual attempts, reported tokens/cost, controlled browser time and
elapsed latency. Auth, expired, drift, missing/thin/ambiguous content, browser,
provider, quota, budget, cancellation and persistence failures remain distinct.

Admin endpoints under /api/admin/job-page-reading:

- GET/PUT root: rollout and limits; PUT includes the observed revision.
- GET /recipes, POST /recipes/{id}/retire: structural evidence and retirement.
- GET /events and /audit: accounting and operator changes.
- POST /canary with a listing URL: read the signed-in operator's saved jobsite
  session in the worker. Returns identity/content/source, failure and metrics.
  It does not score, generate/upload documents, fill forms or submit.

Optional private captures default off, remain owned, are capped at 20/account,
and expire after seven days. Access checks expiry; startup/daily cleanup purges
expired entries. Accounting contains no raw DOM or description. Normal candidate
flows expose neither raw captures nor recipe tables.

## Qualification and release gate

Offline tests cover LinkedIn/SEEK, sibling/lazy/display:contents descriptions,
expansion replacements, sticky/duplicate headers, conflicting IDs, recommendations,
wrong origins, auth/expired/absent pages and hostile plans. Reader tests cover
known/cold/warm/current-text, concurrent misses, cancellation, rollback, live
proof, persistence and invalidation. Model fixtures use local HTTP providers to
measure actual usage without paid calls.

PR #153 is merged, and its migrations 062/063 are integrated before reader
migration 064. Require exact-head CI and offline replay before reader deployment.
Deploy with recovery/reuse off, then qualify the **new code in the production
worker** through the operator's bounded pilot. Keep broad rollout off until the
canary passes. A logged-in dashboard alone does not qualify the new reader or
explain a historical worker failure.

Start production qualification with both switches off. Read the failed listing
in Admin → Automation; record identity, characters, source/failure and metrics.
Enable only the operator's pilot recovery/reuse, then test cold unfamiliar and
different current warm listings. Warm must use zero interpretation requests.
Changed-layout replay must invalidate only incompatible assumptions and re-enter
recovery. If a case fails, preserve an opted-in owned capture, inspect its event
and logs, fix/replay, then expand rollout. Roll back each switch independently.
No real application submission is required for this epic.

LinkedIn description disclosures use trusted pointer input with current owned
control and hit-target rechecks after scroll/hover. The exact physical SDUI More
button may carry aria-hidden on itself; hidden ancestors and application controls
retain strict action checks. A marker alone cannot authorize a non-reading label.
Native extraction accepts complete rendered evidence and excludes hidden, unrelated
and foreign-listing descendants. Busy reading/header regions and real lazy
placeholders block acceptance; unrelated widgets and hidden description templates
do not. DOM-derived caches last only for one synchronous capture evaluation.
