# Epic #127 validation record

The implementation and current release checks are collected in
[PR #138](https://github.com/mmbspk/jobifai/pull/138). The historical checkpoint
was e4aa243; its checks do not qualify later changes. Use the PR's current head
and CI results to identify the exact published revision under review.
Live qualification began on 22 November 2026 using the selected local admin
account. On 23 November, LinkedIn visibly confirmed a real application.
The bot initially missed its native HTML success dialog and
recorded an uncertain outcome. That application is reconciled in history. SEEK
also visibly confirmed a real application on 23 November, using the job-ID
document filenames and automatic library recovery. Its current success-banner
detector is corrected, and the confirmed application is reconciled with its
verified document versions. Final-source release results are recorded on the PR. An opened
form or simulated confirmation is not completion evidence.

## Implemented release behavior

| Stories | Behavior implemented |
| --- | --- |
| #129, #133 | Shared dispatch for dashboard batches, direct apply and approved queue; admin modes, platform overrides, pilots, external flag and transactional audit/history |
| #130, #132 | Fresh scoped controls and signatures covering groups, qualifiers, options and constraints; complete local choice hashes; verified fill retention and approved document uploads |
| #131, #137 | Current profile bindings and explicit candidate facts; checked citations plus independent batched verification; successful grounded fills automatically activate mappings; profile changes invalidate derived answers |
| #132, #133 | Fresh identity and quota checks; bounded execution and durable one-time Submit dispatch; missing confirmation leaves an unknown outcome and prevents resubmission |
| #134 | Scoped Greenhouse forms/actions; source handoff verification and tenant/requisition continuity; separate persisted external identity and scoped confirmation/history |
| #135, #137 | Form-answer task and engine/attempt/platform attribution; scope-aware generation leases, attempt limits and partial field invalidation |

Common recruiter facts are editable during profile creation and later updates:
availability, earliest start date, structured salary, country-specific work rights,
relocation and additional details. Users can extend the profile with custom
questions and answers. Unknown booleans remain unknown; unsupported factual claims
hold instead of being invented. The [profile answer guide](application-profile-answers.md)
records the observed recruiter categories and public sources.

## Verified fixture checkpoints

These checkpoints precede the final commit and exact-SHA CI qualification:

- The complete bot race suite passed in 907.145 seconds with 907 pass events.
  It includes generic phone choices and changed-profile learning, record
  ownership, dynamic rescans and verified-fill cache persistence. Later bounded
  Submit, native confirmation and SEEK recovery changes need final full-source
  qualification.
- The non-bot race suite passed 985 test events without failures; the handler
  package took 315.561 seconds. Later operator-reconciliation regressions passed
  in 14.797 seconds, with handler lint reporting zero issues.
- The current frontend suite passed 390 tests in 49 files, lint and production
  build. All 85 app browser E2E tests passed against a separate fixture server.
  The final Go lint check reports zero issues.
- Contact DOM regressions passed in 17.962 seconds: ISO-only and composite phone
  choices, shared calling codes, field-owned character counters, unrelated page
  progress and nearby controls. “1/4 pages” cannot impose a phone character limit.
- Generic profile-to-choice and grounding checks passed in 7.299 seconds. A
  literal profile fact can become a listed choice only after independent
  verification. Rejected answers cannot activate mappings; warm verified choices
  add zero requests.
- Bounded native Submit tests passed in 12.121 seconds, covering trusted input,
  owned decorations, unrelated covers, hover replacement and one dispatch. Native
  success-dialog worker/history checks passed in 31.997 seconds; scoped status
  and historical captured-layout checks passed in 14.384 seconds.
- Education qualification passed 20 events in 8.678 seconds without skips:
  caption variations preserve the selected record, conflicting current
  attendance holds, and unfamiliar grouped controls retain grounding scope.
- SEEK fixtures cover native uploads, exact approved filenames before Submit,
  attachment mutation holds and verified capacity recovery. Recovery uses
  document IDs and last-use evidence; untracked entries bootstrap from saved
  dates. One selected old attachment is removed per recovery, and every other
  saved ID must remain before upload resumes.
- The job-ID naming regressions cover same-version export across different jobs,
  media and filename safety, preserved renderer-cache hits and historical file
  references. The focused bot suite passed 61 events, including duplicate-upload
  guards and SEEK recovery/review. Applied-document download tests verify both
  new job names and unchanged historical bytes and owner scope.
- Current SEEK sent-banner fixtures require exact job/employer ownership; the
  complete worker records confirmation without relying on a job-page badge.
  The actual private captured success page also passed offline verification.
  Uncertain-result document preservation has cancellation, user, platform and
  listing isolation checks.

## Controlled paired replay

On 23 November 2026, TestPairedReplay_LegacyColdWarmAndPartiallyChangedForm passed
with the Go race detector and dedicated test Chromium. All
browser traffic was intercepted locally; generation caching was enabled for both
engines. Each scenario produced one observed fixture confirmation.

| Scenario | Provider requests | Generation-cache hits |
| --- | ---: | ---: |
| Legacy base | 2 | 0 |
| Adaptive cold | 2 | 0 |
| Adaptive warm | 0 | 0 |
| Adaptive partial form change | 2 | 0 |
| Legacy partial form change | 1 | 2 |
| Adaptive profile change | 0 | 0 |
| Legacy profile change | 2 | 0 |

Adaptive cold and partial attempts include one proposal batch and one independent
verification batch. Successful grounded fills activate mappings automatically;
no manual review occurs between replay scenarios. The changed-profile scenario
reuses direct bindings that reread current facts. It does not demonstrate stale
reuse of derived answers.

See [the benchmark JSON](adaptive-apply-fixture-benchmark.json) for measured
request bytes, browser durations and declared mock token usage. There is one
sample per scenario. Mock tokens are synthetic, production pricing and invoiced
cost are unknown, and these measurements do not establish production latency,
completion rates, median/p95 or confidence intervals.

## Final local release checks

On 23 November, the package suites passed 2,027 test/subtest events across 33
test-bearing packages with the Go race detector, no failed tests and no skipped
tests. Six other packages contain no test files. The full bot package took
1,304.992 seconds; project test and coverage commands now allow 30 minutes.
The final post-submit document-preservation caller also passed its focused
identity and owner-isolation regressions.

Frontend qualification passed all 390 tests in 49 files, ESLint and the production
build. The separate fixture app passed all 85 browser E2E tests. Go lint reports
zero issues. The refreshed paired replay passed all seven scenarios in 26.41
seconds and records the current request bytes; warm interpretation remains zero
requests. Published-revision CI evidence is attached to the current PR head.
Private captures, authentication state and exact candidate values remain outside
the repository.

## Existing-install verification

The managed server runs the worktree binary/frontend with the main checkout's
database, documents and saved browser profiles, listening on 8081. App E2E uses a
separate fixture database/server on 18081. The normal authenticated bot API starts
the same Rod worker used by the dashboard.

The existing database had migration 053 recorded without its audit table or
attempt engine column. An admin pilot write failed and rolled back atomically.
Forward migration 060 conditionally repairs those objects and preserves existing
records. Goose runs are serialized because its legacy Go registry mutates shared
metadata. The SQLite backup passed integrity_check and retained 590 applied-history
records; the runtime database upgraded successfully to 60. The pre-repair backup
remains at data/backups/jobifai-pre-engine-schema-repair-20261122T092717Z.db in the
main checkout. Legacy form-answer profile encoding was also corrected from base64
to a JSON object and has a provider-facing regression.

## Live qualification findings

Pilot overrides select Adaptive for LinkedIn and SEEK for the selected account,
with global Legacy and external destinations disabled. The score threshold
remains 7 and candidate filters are unchanged. The temporary pilot permits one
additional confirmed application. A watcher stops at observed limits of 20
provider calls, $1 estimated incremental inference cost or 900 seconds, or on a
diagnostic failure. These polling limits can overshoot in-flight requests and are
not hard provider ceilings; estimates are not invoiced billing evidence.

| Live run/finding | Result and consequence |
| --- | --- |
| LinkedIn initial discovery | Saved session, search, scoring and filters worked. Salary/connection badges polluted employer metadata; scraper/resolver regressions now preserve company ownership and exclude badges. No confirmation. |
| SEEK Quick Apply | The approved listing opened. The div-based form required native navigation ownership and scoped application-heading resolution. Later attempts verified both tailored documents, then held at employer questions. No confirmation. |
| SEEK transport closure | Browser connection closed before rendering. Session-expiry classification was corrected; this did not prove that the login had expired. |
| LinkedIn standalone SDUI | Plain paragraph titles and hydrated buttons required verified banner/description IDs, unique company/action ownership and bounded trusted clicks. A later run opened Easy Apply. No confirmation. |
| LinkedIn education and contact variations | Both education records completed successfully. Full live choices, record ownership and verified subject categories resolved changed dropdowns. ISO-only phone values and a page-progress counter exposed two generic DOM-reading bugs, now covered by regressions. |
| LinkedIn consent and compensation | Education completed. The role requests biometric and recording consent, plus total compensation. Supplied facts do not establish those declarations or total pay; the application remains held. |
| LinkedIn final review | Contact, approved resume upload and recruiter answers reached final review. The old native Submit click timed out and left an uncertain outcome. Duplicate retry remains blocked. |
| LinkedIn native confirmation | The site showed its application-sent dialog and “Application submitted” status. The run took 90 seconds, 6 provider calls and an estimated $0.181572 delta. The native dialog detector and explicit history reconciliation are being qualified. Exact listing and run identifiers remain in private evidence. |
| SEEK saved-resume capacity | A native Upload action showed the full ten-item library. The user confirmed automatic eviction is intended. Recovery selects by known last use, with saved-date fallback, verifies one deletion and resumes the approved upload. The subsequent live runs verified recovery, both approved attachments and delivery to the employer. |
| SEEK final-review document labels | Both uploads verified and the form reached final review. Plain span labels beneath “Documents included” exposed a semantic-heading assumption. The corrected reader requires one owned document row and the exact approved filename, with duplicate, hidden, wrong-kind and mutation regressions. |
| SEEK native application-sent banner | A subsequent run verified capacity recovery, uploaded the new job-ID filenames, passed final review and reached the exact job's native success route. The visible banner confirmed delivery to the expected employer. The run used zero provider requests and no incremental estimated inference cost. The old detector missed the plain sent banner; its replacement checks the native route, owned heading and exact employer. The operator reconciliation retained both byte-verified document versions. No duplicate submission was issued. |

Candidate-supplied application facts are permanently saved through the normal
profile API. Missing sponsorship, visa subclass and expiry facts have not been
inferred. Personal values remain in private runtime evidence.

## Remaining release evidence

- Both native sites have real confirmation and reconciled history. The final
  native detectors also require final-source regression qualification. Keep any
  other uncertain outcome blocked until independently reconciled.
- Local Go race, frontend, lint, build, app E2E and refreshed paired replay checks
  have passed. Verify the CI results for the exact current published PR head.
- Review privacy and the final diff, commit/push PR #138, and verify CI at the
  exact final SHA. Keep stories and Epic #127 open until their criteria are met.
- Restore temporary pilot settings while preserving intervening user edits and
  candidate facts; disable private diagnostic captures when finished.

## Operating boundaries

Production defaults to Legacy with external follow-through disabled. Greenhouse
is the only hosted external ATS adapter. Consent, absent facts, ambiguous custom
widgets and unmatched autocomplete suggestions hold for review. Native mappings
without a verified issuer link remain listing scoped. Role-dependent fields
include vacancy scope, and country-specific work-rights evidence cannot answer
for another or unknown jurisdiction. Independent model verification cannot grant
upload, navigation or Submit authority.
