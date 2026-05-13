package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
)

const msgQueryError = "query error"

// logParseTime parses an RFC3339 timestamp string, logging a warning on failure.
func logParseTime(s, field string) time.Time {
	t, err := parseTime(s)
	if err != nil {
		log.Warn().Str("field", field).Str("value", s).Msg("jobs: failed to parse timestamp")
	}
	return t
}

// JobHandlers groups job history handlers.
type JobHandlers struct{ svc *Services }

func NewJobHandlers(svc *Services) *JobHandlers { return &JobHandlers{svc: svc} }

func limitOffset(r *http.Request) (limit, offset int) {
	limit, offset = 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// GET /api/jobs/applied
func (h *JobHandlers) Applied(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := limitOffset(r)
	platform := r.URL.Query().Get("platform")

	q := `SELECT id,platform,company,role,COALESCE(location,''),link,
	             COALESCE(resume_path,''),COALESCE(cover_letter_path,''),
	             COALESCE(suitability_score,0),applied_at
	      FROM jobs_applied WHERE user_id = ?`
	args := []any{userID}
	if platform != "" {
		q += " AND platform = ?"
		args = append(args, platform)
	}
	q += " ORDER BY applied_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.svc.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []domain.AppliedJob
	for rows.Next() {
		var j domain.AppliedJob
		var appliedStr string
		if err := rows.Scan(&j.ID, &j.Platform, &j.Company, &j.Role, &j.Location,
			&j.Link, &j.ResumePath, &j.CoverLetterPath, &j.SuitabilityScore, &appliedStr); err != nil {
			log.Error().Err(err).Msg("applied jobs: scan row")
			continue
		}
		j.AppliedAt = logParseTime(appliedStr, "applied_at")
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("applied jobs row iteration error")
		http.Error(w, msgQueryError, http.StatusInternalServerError)
		return
	}
	if out == nil {
		out = []domain.AppliedJob{}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/jobs/skipped
func (h *JobHandlers) Skipped(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := limitOffset(r)
	reason := r.URL.Query().Get("skip_reason")

	q := `SELECT id,platform,company,role,COALESCE(location,''),link,skip_reason,
	             COALESCE(suitability_score,0),COALESCE(suitability_reasoning,''),halal_verdict,viewed_at
	      FROM jobs_skipped
	      WHERE user_id = ? AND skip_reason NOT LIKE 'easy apply:%' AND skip_reason NOT LIKE 'seek apply:%' AND skip_reason NOT LIKE 'quick apply:%'`
	args := []any{userID}
	if reason != "" {
		q += " AND skip_reason LIKE ?"
		args = append(args, "%"+reason+"%")
	}
	q += " ORDER BY viewed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.svc.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []domain.SkippedJob
	for rows.Next() {
		var j domain.SkippedJob
		var viewedStr string
		var halalRaw []byte
		if err := rows.Scan(&j.ID, &j.Platform, &j.Company, &j.Role, &j.Location,
			&j.Link, &j.SkipReason, &j.SuitabilityScore, &j.SuitabilityReasoning,
			&halalRaw, &viewedStr); err != nil {
			log.Error().Err(err).Msg("skipped jobs: scan row")
			continue
		}
		if len(halalRaw) > 0 {
			var v domain.HalalVerdict
			if json.Unmarshal(halalRaw, &v) == nil {
				j.HalalVerdict = &v
			}
		}
		j.ViewedAt = logParseTime(viewedStr, "viewed_at")
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("skipped jobs row iteration error")
		http.Error(w, msgQueryError, http.StatusInternalServerError)
		return
	}
	if out == nil {
		out = []domain.SkippedJob{}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/jobs/cannot-apply
func (h *JobHandlers) CannotApply(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := limitOffset(r)
	platform := r.URL.Query().Get("platform")

	q := `SELECT id,platform,company,role,COALESCE(location,''),link,skip_reason,
	             COALESCE(suitability_score,0),COALESCE(suitability_reasoning,''),halal_verdict,viewed_at
	      FROM jobs_skipped
	      WHERE user_id = ? AND (skip_reason LIKE 'easy apply:%' OR skip_reason LIKE 'seek apply:%' OR skip_reason LIKE 'quick apply:%')`
	args := []any{userID}
	if platform != "" {
		q += " AND platform = ?"
		args = append(args, platform)
	}
	q += " ORDER BY viewed_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.svc.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []domain.SkippedJob
	for rows.Next() {
		var j domain.SkippedJob
		var viewedStr string
		var halalRaw []byte
		if err := rows.Scan(&j.ID, &j.Platform, &j.Company, &j.Role, &j.Location,
			&j.Link, &j.SkipReason, &j.SuitabilityScore, &j.SuitabilityReasoning,
			&halalRaw, &viewedStr); err != nil {
			continue
		}
		if len(halalRaw) > 0 {
			var v domain.HalalVerdict
			if json.Unmarshal(halalRaw, &v) == nil {
				j.HalalVerdict = &v
			}
		}
		j.ViewedAt, _ = parseTime(viewedStr)
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("cannot-apply jobs row iteration error")
		http.Error(w, msgQueryError, http.StatusInternalServerError)
		return
	}
	if out == nil {
		out = []domain.SkippedJob{}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/jobs/cannot-apply/{job_id}/requeue
func (h *JobHandlers) RequeueCannotApply(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var company, role, platform, link, location string
	var score int
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company, role, platform, link, COALESCE(location,''), COALESCE(suitability_score, 0)
		 FROM jobs_skipped
		 WHERE id = ? AND user_id = ? AND (skip_reason LIKE 'easy apply:%' OR skip_reason LIKE 'seek apply:%' OR skip_reason LIKE 'quick apply:%')`,
		jobID, userID).Scan(&company, &role, &platform, &link, &location, &score)
	if err != nil {
		notFound(w, "job not found in cannot-apply list")
		return
	}

	_, err = h.svc.DB.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO jobs_pending_review
		     (job_id, user_id, company, role, location, platform, link, resume_path, cover_letter_path, suitability_score, easy_apply, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, 1, datetime('now'))`,
		jobID, userID, company, role, location, platform, link, score)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	_, _ = h.svc.DB.ExecContext(r.Context(), `DELETE FROM jobs_skipped WHERE id = ? AND user_id = ?`, jobID, userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "requeued"})
}

// GET /api/jobs/top-matches
func (h *JobHandlers) TopMatches(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := limitOffset(r)

	threshold := 7
	var gs domain.GeneralSettings
	if err := h.svc.Config.Get(userID, "general_settings", &gs); err == nil && gs.JobSuitabilityScore > 0 {
		threshold = gs.JobSuitabilityScore
	}

	q := `SELECT job_id,company,role,COALESCE(location,''),platform,COALESCE(link,''),
	             COALESCE(resume_path,''),COALESCE(cover_letter_path,''),
	             suitability_score,COALESCE(suitability_reasoning,''),COALESCE(due_date,''),COALESCE(posted_date,''),
	             easy_apply,COALESCE(halal_verdict,''),created_at
	      FROM jobs_pending_review
	      WHERE user_id = ? AND suitability_score >= ? AND easy_apply = 0
	      ORDER BY suitability_score DESC, created_at DESC
	      LIMIT ? OFFSET ?`

	rows, err := h.svc.DB.QueryContext(r.Context(), q, userID, threshold, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []domain.PendingReview
	for rows.Next() {
		var p domain.PendingReview
		var createdStr string
		var halalJSON string
		if err := rows.Scan(
			&p.JobID, &p.Company, &p.Role, &p.Location, &p.Platform, &p.Link,
			&p.ResumePath, &p.CoverLetterPath,
			&p.SuitabilityScore, &p.SuitabilityReasoning, &p.DueDate, &p.PostedDate,
			&p.EasyApply, &halalJSON, &createdStr,
		); err != nil {
			log.Error().Err(err).Msg("top matches: scan row")
			continue
		}
		p.CreatedAt = logParseTime(createdStr, "created_at")
		if halalJSON != "" {
			var hv domain.HalalVerdict
			if json.Unmarshal([]byte(halalJSON), &hv) == nil {
				p.HalalVerdict = &hv
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("top matches row iteration error")
		http.Error(w, msgQueryError, http.StatusInternalServerError)
		return
	}
	if out == nil {
		out = []domain.PendingReview{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *JobHandlers) Stats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var stats domain.JobStats
	if err := h.svc.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM jobs_applied WHERE user_id = ?", userID).Scan(&stats.TotalApplied); err != nil {
		log.Error().Err(err).Msg("stats: total_applied query failed")
	}
	if err := h.svc.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND date(applied_at) = date('now')", userID).Scan(&stats.AppliedToday); err != nil {
		log.Error().Err(err).Msg("stats: applied_today query failed")
	}
	if err := h.svc.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM jobs_skipped WHERE user_id = ?", userID).Scan(&stats.TotalSkipped); err != nil {
		log.Error().Err(err).Msg("stats: total_skipped query failed")
	}
	writeJSON(w, http.StatusOK, stats)
}

// GET /api/jobs/{job_id}
func (h *JobHandlers) GetJob(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var aj domain.AppliedJob
	var appliedStr string
	var ajHalal []byte
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT id,platform,company,role,COALESCE(location,''),link,
		        COALESCE(resume_path,''),COALESCE(cover_letter_path,''),
		        COALESCE(suitability_score,0),halal_verdict,applied_at
		 FROM jobs_applied WHERE id = ? AND user_id = ?`, jobID, userID,
	).Scan(&aj.ID, &aj.Platform, &aj.Company, &aj.Role, &aj.Location,
		&aj.Link, &aj.ResumePath, &aj.CoverLetterPath, &aj.SuitabilityScore, &ajHalal, &appliedStr)
	if err == nil {
		aj.AppliedAt = logParseTime(appliedStr, "applied_at")
		if len(ajHalal) > 0 {
			var v domain.HalalVerdict
			if json.Unmarshal(ajHalal, &v) == nil {
				aj.HalalVerdict = &v
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": jobID, "status": "applied", "applied_job": aj})
		return
	}

	var sj domain.SkippedJob
	var viewedStr string
	err = h.svc.DB.QueryRowContext(r.Context(),
		`SELECT id,platform,company,role,COALESCE(location,''),link,skip_reason,
		        COALESCE(suitability_score,0),COALESCE(suitability_reasoning,''),viewed_at
		 FROM jobs_skipped WHERE id = ? AND user_id = ?`, jobID, userID,
	).Scan(&sj.ID, &sj.Platform, &sj.Company, &sj.Role, &sj.Location,
		&sj.Link, &sj.SkipReason, &sj.SuitabilityScore, &sj.SuitabilityReasoning, &viewedStr)
	if err == nil {
		sj.ViewedAt = logParseTime(viewedStr, "viewed_at")
		writeJSON(w, http.StatusOK, map[string]any{"id": jobID, "status": "skipped", "skipped_job": sj})
		return
	}

	notFound(w, "job not found")
}

// DELETE /api/jobs/applied/{job_id}
func (h *JobHandlers) DeleteApplied(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")
	res, err := h.svc.DB.ExecContext(r.Context(),
		`DELETE FROM jobs_applied WHERE id = ? AND user_id = ?`, jobID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		notFound(w, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// DELETE /api/jobs/skipped/{job_id}  (covers both Skipped and Cannot Apply)
func (h *JobHandlers) DeleteSkipped(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")
	res, err := h.svc.DB.ExecContext(r.Context(),
		`DELETE FROM jobs_skipped WHERE id = ? AND user_id = ?`, jobID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		notFound(w, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// DELETE /api/jobs/pending-review/{job_id}  (covers Top Matches)
func (h *JobHandlers) DeletePendingReview(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var company, role, location, platform, link, reasoning string
	var score int
	var halalVerdict *string
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company, role, COALESCE(location,''), platform, COALESCE(link,''),
		        suitability_score, COALESCE(suitability_reasoning,''), halal_verdict
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID).
		Scan(&company, &role, &location, &platform, &link, &score, &reasoning, &halalVerdict)
	if err != nil {
		notFound(w, "job not found")
		return
	}

	_, err = h.svc.DB.ExecContext(r.Context(),
		`DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	_, _ = h.svc.DB.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO jobs_skipped
		     (id, user_id, platform, company, role, location, link, skip_reason,
		      suitability_score, suitability_reasoning, halal_verdict, viewed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'Manual skip', ?, ?, ?, datetime('now'))`,
		jobID, userID, platform, company, role, location, link, score, reasoning, halalVerdict)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/jobs/pending-review/{job_id}/mark-applied
func (h *JobHandlers) MarkApplied(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var company, role, location, platform, link string
	var score int
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company, role, COALESCE(location,''), platform, COALESCE(link,''), suitability_score
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID).
		Scan(&company, &role, &location, &platform, &link, &score)
	if err != nil {
		notFound(w, "job not found in top matches")
		return
	}

	_, err = h.svc.DB.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO jobs_applied
		     (id, user_id, platform, company, role, location, link, resume_path, cover_letter_path, suitability_score, applied_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, datetime('now'))`,
		jobID, userID, platform, company, role, location, link, score)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	_, _ = h.svc.DB.ExecContext(r.Context(),
		`DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// POST /api/jobs/pending-review/{job_id}/blacklist
func (h *JobHandlers) BlacklistCompany(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var company string
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		jobID, userID).Scan(&company)
	if err != nil {
		notFound(w, "job not found in top matches")
		return
	}

	var prefs domain.WorkPreferences
	_ = h.svc.Config.Get(userID, keyWorkPreferences, &prefs)

	already := false
	for _, c := range prefs.CompanyBlacklist {
		if strings.EqualFold(c, company) {
			already = true
			break
		}
	}
	if !already {
		prefs.CompanyBlacklist = append(prefs.CompanyBlacklist, company)
		if err := h.svc.Config.Set(userID, keyWorkPreferences, prefs); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}

	_, _ = h.svc.DB.ExecContext(r.Context(),
		`DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "blacklisted"})
}
