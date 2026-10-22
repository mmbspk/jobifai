package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// BotHandlers groups bot lifecycle and review-gate handlers.
type BotHandlers struct{ svc *Services }

func NewBotHandlers(svc *Services) *BotHandlers { return &BotHandlers{svc: svc} }

const msgBotNotInit = "bot not initialised"

// POST /api/bot/start
func (h *BotHandlers) Start(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Platform domain.Platform `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Platform == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "platform is required"})
		return
	}
	if h.svc.Quota != nil {
		st, err := h.svc.Quota.Status(r.Context(), userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
		if st.Blocked {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"code":    st.BlockCode,
				"message": "AI usage limit reached — subscribe to continue",
			})
			return
		}
	}
	if err := h.svc.Bot.Start(r.Context(), userID, req.Platform); err != nil {
		conflict(w, err.Error())
		return
	}
	okMsg(w, "bot started")
}

// POST /api/bot/stop
func (h *BotHandlers) Stop(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	status := h.svc.Bot.Status(userID)
	if status.State != domain.BotStateRunning && status.State != domain.BotStatePaused {
		notFound(w, "bot is not running")
		return
	}
	h.svc.Bot.Stop(userID)
	if h.svc.Quota != nil {
		h.svc.Quota.EndSubscriberSession(userID)
	}
	okMsg(w, "stop signal sent")
}

// POST /api/bot/pause
func (h *BotHandlers) Pause(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	status := h.svc.Bot.Status(userID)
	if status.State != domain.BotStateRunning {
		notFound(w, "bot is not running")
		return
	}
	h.svc.Bot.Pause(userID)
	okMsg(w, "pause signal sent")
}

// POST /api/bot/resume
func (h *BotHandlers) Resume(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	status := h.svc.Bot.Status(userID)
	if status.State != domain.BotStatePaused {
		notFound(w, "bot is not paused")
		return
	}
	h.svc.Bot.Resume(userID)
	okMsg(w, "resume signal sent")
}

// GET /api/bot/status
func (h *BotHandlers) Status(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.Bot == nil {
		s := domain.BotStatus{State: domain.BotStateIdle}
		gs := config.ResolveOperationalSettings(h.svc.Config, userID, false)
		s.DailyLimit = gs.HumanBehavior.DailyApplicationLimit
		writeJSON(w, http.StatusOK, s)
		return
	}
	writeJSON(w, http.StatusOK, h.svc.Bot.Status(userID))
}

// GET /api/bot/run-logs
func (h *BotHandlers) RunLogs(w http.ResponseWriter, r *http.Request) {
	if h.svc.BotRunLogs == nil {
		writeJSON(w, http.StatusOK, domain.BotRunLogs{Lines: []domain.BotRunLog{}})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	result, err := h.svc.BotRunLogs.LatestRun(r.Context(), userID)
	if err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("run-logs: LatestRun")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to fetch run logs"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GET /api/bot/review/pending
func (h *BotHandlers) ReviewListPending(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := h.svc.DB.QueryContext(r.Context(),
		`SELECT job_id,company,role,COALESCE(location,''),platform,link,resume_path,cover_letter_path,
		        COALESCE(resume_content_version_id,''),COALESCE(cover_letter_content_version_id,''),COALESCE(document_refs_json,''),
		        suitability_score,easy_apply,COALESCE(halal_verdict,''),created_at
		 FROM jobs_pending_review WHERE user_id = ? AND easy_apply = 1 AND NOT EXISTS (SELECT 1 FROM jobs_skipped s WHERE s.user_id=jobs_pending_review.user_id AND s.id=jobs_pending_review.job_id) ORDER BY created_at DESC`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer func() { _ = rows.Close() }()

	var out []domain.PendingReview
	for rows.Next() {
		var p domain.PendingReview
		var createdStr, halalJSON string
		if err := rows.Scan(&p.JobID, &p.Company, &p.Role, &p.Location, &p.Platform,
			&p.Link, &p.ResumePath, &p.CoverLetterPath, &p.ResumeContentVersionID, &p.CoverLetterContentVersionID, &p.DocumentRefsJSON,
			&p.SuitabilityScore, &p.EasyApply, &halalJSON, &createdStr); err != nil {
			log.Error().Err(err).Msg("review pending: scan row")
			continue
		}
		p.CreatedAt, _ = parseTime(createdStr)
		if halalJSON != "" {
			var hv domain.HalalVerdict
			if json.Unmarshal([]byte(halalJSON), &hv) == nil {
				p.HalalVerdict = &hv
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("review pending: row iteration error")
		http.Error(w, msgQueryError, http.StatusInternalServerError)
		return
	}
	if out == nil {
		out = []domain.PendingReview{}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/bot/review/{job_id}/prepare
func (h *BotHandlers) ReviewPrepareDocuments(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")
	if err := h.svc.Bot.PrepareReviewDocuments(r.Context(), userID, jobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "documents prepared")
}

// POST /api/bot/review/{job_id}/approve
func (h *BotHandlers) ReviewApprove(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var req bot.SubmitRequest
	req.JobID = jobID
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company,role,COALESCE(location,''),platform,link,COALESCE(resume_path,''),COALESCE(cover_letter_path,''),
		        COALESCE(resume_content_version_id,''),COALESCE(cover_letter_content_version_id,''),COALESCE(document_refs_json,''),
		        COALESCE(suitability_reasoning,'')
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID,
	).Scan(&req.Company, &req.Role, &req.Location, &req.Platform, &req.Link, &req.ResumePath, &req.CoverPath,
		&req.ResumeContentVersionID, &req.CoverContentVersionID, &req.DocumentRefsJSON, &req.SuitabilityReasoning)
	if err != nil {
		notFound(w, "no pending review for job_id "+jobID)
		return
	}
	pack := documents.ParseApplicationPackJSON(req.DocumentRefsJSON)
	if !pack.ReadyForSubmit() {
		msg := "prepare documents before approving this application"
		if pack.HoldReason != "" {
			msg = pack.HoldReason
		}
		writeJSON(w, http.StatusConflict, map[string]string{"message": msg})
		return
	}
	req.FrozenDocuments = true

	if err := h.svc.Bot.SubmitSync(r.Context(), userID, req); err != nil {
		if errors.Is(err, quota.ErrExceeded) {
			log.Warn().Str("user_id", userID).Str("job_id", jobID).Msg("review approve: quota exceeded")
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits — top up your balance and try again"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if _, err := appdb.ExecContextWithRetry(r.Context(), h.svc.DB,
		"DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?", jobID, userID); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review approve: failed to delete pending review after submission")
	}
	okMsg(w, "application submitted")
}

// POST /api/bot/review/{job_id}/reject
func (h *BotHandlers) ReviewReject(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var company, role, platform, link, location string
	var score int
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company,role,platform,link,COALESCE(location,''),COALESCE(suitability_score,0)
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID,
	).Scan(&company, &role, &platform, &link, &location, &score)
	if err != nil {
		notFound(w, "no pending review for job_id "+jobID)
		return
	}

	if _, err := appdb.ExecContextWithRetry(r.Context(), h.svc.DB,
		`INSERT OR IGNORE INTO jobs_skipped
		 (id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
		 VALUES(?,?,?,?,?,?,?,'manual_reject',?,'',datetime('now'))`,
		jobID, userID, platform, company, role, location, link, score,
	); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review reject: failed to insert skipped job")
	}
	if _, err := appdb.ExecContextWithRetry(r.Context(), h.svc.DB,
		"DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?", jobID, userID); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review reject: failed to delete pending review")
	}

	log.Info().Str("user_id", userID).Str("job_id", jobID).Str("company", company).Str("role", role).Msg("review reject: job manually rejected and moved to Cannot Apply")
	okMsg(w, "rejected, job will not be reprocessed")
}

// POST /api/bot/apply-url
func (h *BotHandlers) ApplyURL(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		URL    string `json:"url"`
		Market string `json:"market"`
		Force  bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "url is required"})
		return
	}
	log.Info().Str("user_id", userID).Str("url", req.URL).Msg("apply-url: starting AI Apply")
	// Bound total AI Apply time so the UI cannot sit on "Submitting…" forever
	// (browser hangs, LLM stalls, or form loops).
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	res, err := h.svc.Bot.ApplyFromURL(ctx, userID, req.URL, req.Market, req.Force)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			log.Info().Str("user_id", userID).Str("url", req.URL).Msg("apply-url: AI Apply cancelled")
			writeJSON(w, http.StatusOK, map[string]string{"message": "AI Apply cancelled"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn().Str("user_id", userID).Str("url", req.URL).Msg("apply-url: AI Apply timed out after 4 minutes")
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{
				"message": "AI Apply timed out after 4 minutes — try again, or confirm the job has Easy Apply / Quick Apply",
			})
			return
		}
		if errors.Is(err, bot.ErrNotEasyApply) {
			log.Info().Str("user_id", userID).Str("url", req.URL).Str("company", res.Company).Str("role", res.Role).Msg("apply-url: not Easy Apply, added to Top Matches")
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "not_easy_apply",
				"message": "This job does not have Easy Apply / Quick Apply. It has been added to your Top Matches for manual application.",
				"company": res.Company,
				"role":    res.Role,
			})
			return
		}
		if errors.Is(err, bot.ErrAlreadyApplied) {
			log.Info().Str("user_id", userID).Str("url", req.URL).Str("company", res.Company).Str("role", res.Role).Msg("apply-url: already applied")
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "already_applied",
				"company": res.Company,
				"role":    res.Role,
			})
			return
		}
		if errors.Is(err, quota.ErrExceeded) {
			log.Warn().Str("user_id", userID).Str("url", req.URL).Msg("apply-url: quota exceeded")
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits — top up your balance and try again"})
			return
		}
		log.Error().Err(err).Str("user_id", userID).Str("url", req.URL).Str("company", res.Company).Str("role", res.Role).Msg("apply-url: AI Apply failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if res.ScoreWarning {
		log.Info().Str("user_id", userID).Str("url", req.URL).Str("job_id", res.JobID).Int("score", res.Score).Msg("apply-url: score below threshold, not applying")
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "score_warning",
			"score":     res.Score,
			"reasoning": res.ScoreReason,
			"job_id":    res.JobID,
			"company":   res.Company,
			"role":      res.Role,
		})
		return
	}
	log.Info().Str("user_id", userID).Str("url", req.URL).Str("job_id", res.JobID).Str("company", res.Company).Str("role", res.Role).Msg("apply-url: submitted ✓")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "applied",
		"message": "Application submitted",
		"company": res.Company,
		"role":    res.Role,
	})
}
