# LinkedIn listing identity recovery

The resolver classifies only the intended job's verified details pane. A different
`currentJobId` is never evidence about the original `referenceJobId`. Classification
requires the individual `/jobs/view/<intended-id>` page and matching rendered content.
Top Matches contains only evaluated listings at or above the configured suitability
threshold. Technical failures appear in Cannot Apply with a friendly reason and
structured diagnostic details. Verified job content is scored
and filtered even when its application control is unrecognized. An unknown
control never grants permission to click or apply. If identity itself cannot be
verified, no scoring occurs. Missing descriptions or unavailable scoring also
block qualification. Diagnostics are retained in jobs_skipped.failure_details_json.
The evaluation input includes the actual description plus title, company,
location and available header metadata. Three consecutive identical technical
failures on distinct jobs stop the platform run for investigation.

## SDUI layout regression

The October 16 captures reproduced `job_pane_not_unique` on both a native and
an external listing. Their header uses opaque CSS classes, a paragraph title
instead of the old job-title link, and no legacy pane selectors. The resolver
now identifies the smallest header combining a company accessible label with
canonical links to the intended job, and requires a unique matching
`JobDetails_AboutTheJob_<id>` description. Wrapper classes, sibling positions
and div depth do not establish ownership. Only that header is scanned for controls; `JobDetailsSimilarJobsSlot_`
recommendations are excluded. Description extraction uses the matching About
section. Reduced, tracking-free versions of both captures live in
`internal/bot/testdata/linkedin/` and are exercised by DOM and browser tests.

## Existing Cannot Apply entries

The fix does not automatically retry historical applications. Stop automation,
back up the live SQLite database using SQLite's backup API (including committed
WAL data), and preview the explicitly selected records:

```sh
python3 scripts/recover_linkedin_identity.py --db data/jobifai.db --user __default__ \
  --job-id 4478815983 --job-id 4469906630 --job-id 4476251428 \
  --job-id 4448435156 --job-id 4468767275 --job-id 4479762073 \
  --job-id 4472390955 --job-id 4453130324 --job-id 4479748087
```

Review the output. To apply the same selection, add
`--apply --backup /absolute/path/linkedin-original-skips.json`. The archive must
not already exist. All changes run in one SQLite transaction. The command only
accepts historical LinkedIn identity-error skips, excludes approved/in-progress/
applied/uncertain submissions, preserves attempt history and existing prepared
documents, and queues eligible rows for fresh classification and evaluation.
Migration 050 must already be installed. The script itself does not click or apply;
the next bot run may submit a qualifying native job, according to the user's normal
review settings. External jobs enter Top Matches only after evaluation and filters.
Restart automation afterwards to reload its seen cache. The UI's Re-evaluate action
uses the same evaluation-first queue; uncertain submissions cannot be retried there.
Reconcile those manually on the original site before taking further action.

Migration 050 also moves older unresolved Top Matches holds into Cannot Apply
without deleting their prepared documents or retrying applications.

## Validation before broad automation

Use an authenticated browser through the existing application session/profile
loader. Verify a known external listing and a known native listing with the new
resolver, without submitting applications. Record the classification diagnostic:
intended ID, observed ID, control label/destination, and selection reason. The
fixture suite verifies the regression mechanics but is not a captured sample of
every current LinkedIn interface variant. Unknown variants must remain visible
in Cannot Apply with an explicit reason instead of being guessed. Before enabling
broad auto-apply, also run one explicitly authorized native end-to-end smoke test;
fixture submissions use only local synthetic forms.

Seek shares the evaluation-first retry and missing-description/scorer guards.
Its selectors and application identity logic have not been verified by these
LinkedIn captures. No claim about live Seek behavior follows from these fixtures.

## Admin follow-up

[Issue #104](https://github.com/mmbspk/jobifai/issues/104) covers grouped incidents,
sanitized evidence links and optional rate-limited email alerts. The recommended
source of truth is an Admin incident list, with email used for serious grouped
failures or platform pauses. This PR does not send admin emails or implement that UI.
