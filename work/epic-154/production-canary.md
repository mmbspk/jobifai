# Epic 154 production canary — 26 November 2026

Deployed build: 2dc48e8903ba557b0e5bd5af0e4fbf109cf6694e (merged PR 163
plus build-version reporting). Its reader source matches qualified head 05d23ca.
Operator: admin@jobifai.local. All tests below used the dedicated read-only
worker canary; no applications were submitted by the canary.

## Observed worker outcomes

Recovery/reuse initially off, as deployed:

| Listing | Result | Description characters | AI requests |
|---|---|---:|---:|
| LinkedIn 4494759546, Sharon AI | description_missing | 0 | 0 |
| LinkedIn 4491867472, Midnite | description_incomplete | 0 | 0 |
| LinkedIn 4484814584, dentsu | description_incomplete | 0 | 0 |
| SEEK 96103223, RAA | identity_drift | 0 | 0 |

Temporary pilot: only __default__; two requests and USD 0.15 maximum model
cost per read, with bounded owned captures enabled for diagnosis:

| Listing | Result | Description characters | AI requests |
|---|---|---:|---:|
| LinkedIn 4494759546, Sharon AI | deterministic success | 5526 | 0 |
| LinkedIn 4491867472, Midnite | identity_unverified | 0 | 0 |
| SEEK 96007786, G'day Group | identity_drift | 0 | 0 |

Sharon's successful subsequent deterministic read does not demonstrate model
recovery or learned recipe reuse. No paid model request occurred. Browser usage
followed normal account metering. The original rollout/limits/capture settings
were restored with the configuration revision guard after diagnosis.

## Reproduced causes

SEEK target lifecycle events in the same production worker browser show the
job navigation landing on /browser-upgrade, then closing before extraction.
The shared reader did not invoke the compatibility handler used by other paths.
Canonical www.seek.com.au and au.seek.com aliases also need the same issuer
boundary when acknowledging that notice; AU and NZ remain distinct.

Midnite's production header renders Senior Backend Engineer as a paragraph.
Its document title is Senior Backend Engineer | Midnite | LinkedIn, and its
job-ID-tagged description is a sibling section containing an expansion control.
The URL-only native adapter left title empty, preventing the independently
validated capture from establishing header identity when recovery was needed.

The user's failed AI Apply task targets SEEK 95987525. Production attempt logs
record blocked/extraction/listing_unverified before suitability evaluation or
submission. The intended job is live: Backend Developer at m-View Live Video
Pty Ltd, with 5454 description characters. Its rendered page has no JobPosting
JSON-LD or job-ID attribute. The visible primary job-detail-apply anchor explicitly
references /job/95987525/apply; title, company and description use native semantic
attributes. The old capture required an attribute/schema identifier and rejected
this valid client-rendered page.

## Follow-up implementation and evidence

- Run bounded SEEK compatibility preparation inside the measured Known stage,
  preserving cancellation, quota attribution and typed browser failures.
- Accept canonical/regional aliases within one HTTPS SEEK issuer. Reject a
  different issuer, port or intended listing before acknowledging the notice.
- Use LinkedIn document metadata only as a hint for an independently matching,
  visible owned paragraph title and company. Preserve declared-title behavior.
- Corroborate SEEK's explicit primary job reference with visible primary title
  and company fields. An empty/query/fragment reference cannot borrow the current
  URL's ID; recommendations and description text cannot lend header evidence.
- Keep header evidence outside description blocks so repeated job titles do not
  remove the full description. Reference tracking parameters never enter snapshots.

The new fixtures failed before the fixes. Final local qualification passed:
shared reader race suite 80.135s; reader worker hooks plus compatibility and
captured submission guards 61.281s; browser package race suite 11.016s; Go lint
zero issues. A read-only replay of the corrected capture script against the
actual production SEEK page identified job 95987525, Backend Developer,
m-View Live Video Pty Ltd, and its complete visible 5454-character description.
That script replay is diagnostic evidence, not a deployed application success.

The follow-up branch is codex/epic-154-production-canary-fixes. Exact-head CI,
review/merge/deployment and repeated production worker/API qualification remain
required. Keep Story 161 and Epic 154 open until those checks pass.

## Repeat canary on merged PR 166

The production worker reported build **83b9999**, the merge of PR 166.
Recovery and reuse initially remained off:

| Listing | Result | Description characters | AI requests |
|---|---|---:|---:|
| SEEK 95987525, the user's failed AI Apply listing | deterministic success | 5454 | 0 |
| SEEK 96103223, RAA | deterministic success | 4092 | 0 |
| SEEK 96007786, G'day Group | deterministic success | 4361 | 0 |
| LinkedIn 4494759546, Sharon AI | deterministic success | 5526 | 0 |
| LinkedIn 4491867472, Midnite | description_incomplete | 0 | 0 |
| LinkedIn 4484814584, dentsu | description_incomplete | 0 | 0 |

An operator-only pilot with recovery/reuse and bounded private captures enabled
failed on Midnite: reading_deadline_exceeded after 58,132 ms, four local recovery
actions, zero proposal/verification/HTTP requests and zero model cost. The stored
owned capture verified Senior Backend Engineer at Midnite, but its 4,968-character
description remained collapsed. No expansion action was exposed; Loading was true,
and recovery spent its actions scrolling populated description/candidate regions.
The original production rollout, limits, pilot membership and capture setting were
restored with the latest configuration revision before offline implementation.
No application or document generation/upload occurred during these canaries.

A subsequent read-only inspection of the logged-in browser's current Midnite DOM
confirmed a physically rendered button inside JobDetails_AboutTheJob_4491867472:
data-testid=expandable-text-button, aria-hidden=true, text "… more", display:block,
visibility:visible, approximately 400 by 21 pixels. The description height was
209 pixels. This local-browser observation explains failed expansion discovery;
it is separate from the worker canary and does not establish a successful live
expansion or the exact worker loading widget.

The next focused fix addresses physical More-button discovery, trusted native
expansion, complete-only native description evidence, relevant loading scope and
repeated DOM traversal. A nested SDUI regression demonstrated 120,799 computed
style reads before invocation-local memoization and 3,557 after it (428 ms versus
149 ms in that fixture). Caches remain per evaluation; replaced nodes and changed
DOM still require current proof. Those timings are offline measurements.

A new merged/deployed build and repeat worker qualification are still required,
including Midnite and dentsu, model-assisted cold recovery and different current
warm listings. Story 161 and Epic 154 remain open.

The follow-up branch is codex/epic-154-linkedin-recovery. Its regressions cover
trusted expansion, replaced/renamed controls, overlays, hidden/foreign descendants,
hidden templates, busy-content waiting, semantic line breaks and clipped tails.
Independent review reproductions now pass with no remaining actionable findings.
Local qualification: full shared-reader race suite 111.535s; affected native
resolver/expansion/ownership race suite 109.543s; final ownership/native-pointer
race subset 19.887s after invocation-local heading-cache refinement. Captured
scoring/submission guards passed in 68.096s, worker reader hooks in 57.857s,
and the final shared clipping/performance subset in 17.434s. Go lint reports
zero issues. These are fixture results; the new code has not run in production.
