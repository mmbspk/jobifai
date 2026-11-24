package bot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/user/jobifai/internal/domain"
)

// Keep the already prepared references available for explicit reconciliation.
// This cannot generate documents, authorize retry or mark an application sent.
func (b *Bot) preserveUnknownSubmissionDocuments(ctx context.Context, platform domain.Platform, job linkedInJob, lazy *lazyDocGen, score int, reasoning string, verdict []byte) {
	if b.cfg.DB == nil || lazy == nil {
		return
	}
	resume, cover := lazy.peek()
	if resume == "" && cover == "" && lazy.resumeVersionID == "" && lazy.coverVersionID == "" {
		return
	}
	refs := map[string]any{}
	_ = json.Unmarshal([]byte(lazy.refsJSON), &refs)
	if refs == nil {
		refs = map[string]any{}
	}
	refs["hold_reason"] = "Submission could not be confirmed. Check the original listing before retrying."
	refs["listing_review_required"] = true
	refs["submission_unknown"] = true
	refsJSON, _ := json.Marshal(refs)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := b.cfg.DB.ExecContext(ctx, `INSERT INTO jobs_pending_review
 (job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,
 resume_content_version_id,cover_letter_content_version_id,document_refs_json,suitability_score,suitability_reasoning,easy_apply,halal_verdict,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,datetime('now'))
 ON CONFLICT(job_id) DO UPDATE SET easy_apply=0,
 resume_path=CASE WHEN excluded.resume_path<>'' THEN excluded.resume_path ELSE jobs_pending_review.resume_path END,
 cover_letter_path=CASE WHEN excluded.cover_letter_path<>'' THEN excluded.cover_letter_path ELSE jobs_pending_review.cover_letter_path END,
 resume_content_version_id=CASE WHEN excluded.resume_content_version_id<>'' THEN excluded.resume_content_version_id ELSE jobs_pending_review.resume_content_version_id END,
 cover_letter_content_version_id=CASE WHEN excluded.cover_letter_content_version_id<>'' THEN excluded.cover_letter_content_version_id ELSE jobs_pending_review.cover_letter_content_version_id END,
 document_refs_json=json_patch(CASE WHEN json_valid(jobs_pending_review.document_refs_json) THEN jobs_pending_review.document_refs_json ELSE '{}' END,excluded.document_refs_json),
 halal_verdict=COALESCE(excluded.halal_verdict,jobs_pending_review.halal_verdict)
 WHERE jobs_pending_review.user_id=excluded.user_id AND jobs_pending_review.platform=excluded.platform
 AND (COALESCE(jobs_pending_review.link,'')='' OR jobs_pending_review.link=excluded.link)`,
		job.ID, b.cfg.UserID, job.Company, job.Title, job.Location, platform, job.URL, resume, cover,
		lazy.resumeVersionID, lazy.coverVersionID, string(refsJSON), score, reasoning, nilIfEmpty(verdict))
	if err != nil {
		b.log.Error().Err(err).Str("job_id", job.ID).Msg("could not preserve uncertain submission documents")
	}
}

func nilIfEmpty(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}
