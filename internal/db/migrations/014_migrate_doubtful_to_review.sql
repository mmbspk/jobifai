-- +goose Up
-- DOUBTFUL halal-filter skips passed the suitability threshold but were incorrectly blocked.
-- Move them into jobs_pending_review so they appear in Top Matches for the user to decide.
INSERT OR IGNORE INTO jobs_pending_review
    (job_id, user_id, company, role, location, platform, link,
     resume_path, cover_letter_path, suitability_score, suitability_reasoning,
     due_date, posted_date, easy_apply, halal_verdict, created_at)
SELECT
    id, user_id, company, role, COALESCE(location, ''), platform, link,
    '', '', COALESCE(suitability_score, 0), COALESCE(suitability_reasoning, ''),
    '', '', 0, halal_verdict, viewed_at
FROM jobs_skipped
WHERE skip_reason = 'halal filter'
  AND json_extract(halal_verdict, '$.verdict') = 'DOUBTFUL';

DELETE FROM jobs_skipped
WHERE skip_reason = 'halal filter'
  AND json_extract(halal_verdict, '$.verdict') = 'DOUBTFUL';

-- +goose Down
-- No rollback: the original jobs_skipped rows are gone after the migration runs.
