-- +goose Up
-- Apply failures caused by external ATS / bot protection are manual-apply jobs,
-- not true Quick/Easy Apply failures. Move them from Cannot Apply to Top Matches.
INSERT OR REPLACE INTO jobs_pending_review
    (job_id, user_id, company, role, location, platform, link,
     resume_path, cover_letter_path, suitability_score, suitability_reasoning,
     due_date, posted_date, easy_apply, halal_verdict, created_at)
SELECT id, user_id, company, role, COALESCE(location, ''), platform, link,
       '', '', COALESCE(suitability_score, 0), COALESCE(suitability_reasoning, ''),
       '', '', 0, halal_verdict, viewed_at
FROM jobs_skipped
WHERE (skip_reason LIKE 'seek apply:%' OR skip_reason LIKE 'easy apply:%' OR skip_reason LIKE 'quick apply:%')
  AND (
    lower(skip_reason) LIKE '%blocked%'
    OR lower(skip_reason) LIKE '%smartrecruiters%'
    OR lower(skip_reason) LIKE '%external ats%'
    OR lower(skip_reason) LIKE '%external site%'
    OR lower(skip_reason) LIKE '%datadome%'
    OR lower(skip_reason) LIKE '%captcha%'
    OR lower(skip_reason) LIKE '%not automatable%'
  );

DELETE FROM jobs_skipped
WHERE (skip_reason LIKE 'seek apply:%' OR skip_reason LIKE 'easy apply:%' OR skip_reason LIKE 'quick apply:%')
  AND (
    lower(skip_reason) LIKE '%blocked%'
    OR lower(skip_reason) LIKE '%smartrecruiters%'
    OR lower(skip_reason) LIKE '%external ats%'
    OR lower(skip_reason) LIKE '%external site%'
    OR lower(skip_reason) LIKE '%datadome%'
    OR lower(skip_reason) LIKE '%captcha%'
    OR lower(skip_reason) LIKE '%not automatable%'
  );

-- +goose Down
-- No rollback: rows were removed from jobs_skipped after copy to pending_review.
