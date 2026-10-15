# LinkedIn listing identity recovery

The resolver classifies only the intended job's verified details pane. A different
`currentJobId` is never evidence about the original `referenceJobId`. A collection
with the intended selected ID is usable only when the rendered pane also matches.
Unresolved listings appear in Top Matches with an explicit manual-verification
message; their suitability is not fabricated. Verified external listings still
pass the normal score and halal filters before entering Top Matches.

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
documents, and moves eligible rows to manual review. It does not click or apply.
Restart automation afterwards to reload its seen cache.

## Validation before broad automation

Use an authenticated browser through the existing application session/profile
loader. Verify a known external listing and a known native listing with the new
resolver, without submitting applications. Record the classification diagnostic:
intended ID, observed ID, control label/destination, and selection reason. The
fixture suite verifies the regression mechanics but is not a captured sample of
every current LinkedIn interface variant. Unknown variants must remain visible
for manual review instead of being guessed or permanently skipped.

Seek is unchanged and has not been verified by this LinkedIn investigation.
