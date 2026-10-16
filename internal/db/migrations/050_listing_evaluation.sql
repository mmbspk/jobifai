-- +goose Up
ALTER TABLE jobs_approved_queue ADD COLUMN needs_evaluation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs_skipped ADD COLUMN failure_code TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs_skipped ADD COLUMN failure_details_json TEXT NOT NULL DEFAULT '';

-- Older builds put unresolved listings in Top Matches. Retain prepared documents
-- in pending_review, but surface those holds in Cannot Apply. No applications
-- are retried by this migration.
INSERT OR IGNORE INTO jobs_skipped
 (id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at,failure_code,failure_details_json)
SELECT p.job_id,p.user_id,p.platform,p.company,p.role,p.location,p.link,
 'easy apply: [listing_verification_required] ' || COALESCE(json_extract(CASE WHEN json_valid(p.document_refs_json) THEN p.document_refs_json ELSE '{}' END,'$.hold_reason'),'Listing verification could not be completed. Re-evaluate after the issue is resolved.'),
 p.suitability_score,p.suitability_reasoning,p.halal_verdict,datetime('now'),
 CASE WHEN lower(COALESCE(json_extract(CASE WHEN json_valid(p.document_refs_json) THEN p.document_refs_json ELSE '{}' END,'$.hold_reason'),'')) LIKE '%submission%' THEN 'submission_unknown' ELSE 'listing_verification_required' END,COALESCE(p.document_refs_json,'{}')
FROM jobs_pending_review p
WHERE p.platform='linkedin'
 AND json_extract(CASE WHEN json_valid(p.document_refs_json) THEN p.document_refs_json ELSE '{}' END,'$.listing_review_required')=1
 AND COALESCE(json_extract(CASE WHEN json_valid(p.document_refs_json) THEN p.document_refs_json ELSE '{}' END,'$.hold_reason'),'')<>'Apply on the employer''s website.'
 AND NOT EXISTS (SELECT 1 FROM jobs_applied a WHERE a.user_id=p.user_id AND (a.id=p.job_id OR a.link=p.link))
 AND NOT EXISTS (SELECT 1 FROM jobs_approved_queue q WHERE q.user_id=p.user_id AND q.job_id=p.job_id)
 AND NOT EXISTS (SELECT 1 FROM apply_attempts t WHERE t.user_id=p.user_id AND (t.job_id=p.job_id OR t.url=p.link) AND t.outcome='in_progress');

-- Legacy retries without a scored review record must re-enter evaluation.
UPDATE jobs_approved_queue SET needs_evaluation=1
WHERE NOT EXISTS (SELECT 1 FROM jobs_pending_review p WHERE p.user_id=jobs_approved_queue.user_id AND p.job_id=jobs_approved_queue.job_id AND p.suitability_score>0);

-- +goose Down
ALTER TABLE jobs_skipped DROP COLUMN failure_details_json;
ALTER TABLE jobs_skipped DROP COLUMN failure_code;
ALTER TABLE jobs_approved_queue DROP COLUMN needs_evaluation;
