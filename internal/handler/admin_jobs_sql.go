package handler

// Pipeline counts align with product handlers (jobs.go, bot.go):
// - pending_review: jobs_pending_review WHERE easy_apply = 1
// - top_matches: jobs_pending_review WHERE easy_apply = 0
// - skipped: jobs_skipped excluding cannot-apply skip reasons
// - cannot_apply: jobs_skipped matching sqlCannotApplyFilter

const sqlSkippedNormalOnly = `NOT (` + sqlCannotApplyFilter + `)`
