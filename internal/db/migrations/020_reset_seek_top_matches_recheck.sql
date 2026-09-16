-- +goose Up
-- Allow Seek top-matches recheck to retry jobs that were misclassified before
-- broader Quick Apply detection (Seek-hosted /apply without "quick apply" label).
UPDATE jobs_pending_review
SET attempt_count = 0
WHERE platform = 'seek' AND easy_apply = 0 AND attempt_count > 0;

-- +goose Down
-- No rollback: attempt_count values before migration are unknown.
