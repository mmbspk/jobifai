package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
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
		var gs domain.GeneralSettings
		if err := h.svc.Config.Get(userID, "general_settings", &gs); err == nil {
			s.DailyLimit = gs.HumanBehavior.DailyApplicationLimit
		}
		writeJSON(w, http.StatusOK, s)
		return
	}
	writeJSON(w, http.StatusOK, h.svc.Bot.Status(userID))
}

// GET /api/bot/review/pending
func (h *BotHandlers) ReviewListPending(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := h.svc.DB.QueryContext(r.Context(),
		`SELECT job_id,company,role,COALESCE(location,''),platform,link,resume_path,cover_letter_path,suitability_score,easy_apply,COALESCE(halal_verdict,''),created_at
		 FROM jobs_pending_review WHERE user_id = ? AND easy_apply = 1 ORDER BY created_at DESC`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []domain.PendingReview
	for rows.Next() {
		var p domain.PendingReview
		var createdStr, halalJSON string
		if err := rows.Scan(&p.JobID, &p.Company, &p.Role, &p.Location, &p.Platform,
			&p.Link, &p.ResumePath, &p.CoverLetterPath, &p.SuitabilityScore, &p.EasyApply, &halalJSON, &createdStr); err != nil {
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

// POST /api/bot/review/{job_id}/approve
func (h *BotHandlers) ReviewApprove(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")

	var req bot.SubmitRequest
	req.JobID = jobID
	err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT company,role,COALESCE(location,''),platform,link,COALESCE(resume_path,''),COALESCE(cover_letter_path,''),COALESCE(suitability_reasoning,'')
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID,
	).Scan(&req.Company, &req.Role, &req.Location, &req.Platform, &req.Link, &req.ResumePath, &req.CoverPath, &req.SuitabilityReasoning)
	if err != nil {
		notFound(w, "no pending review for job_id "+jobID)
		return
	}

	if _, err := h.svc.DB.ExecContext(r.Context(),
		"DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?", jobID, userID); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review approve: failed to delete pending review")
	}

	if err := h.svc.Bot.SubmitSync(r.Context(), userID, req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
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

	if _, err := h.svc.DB.ExecContext(r.Context(),
		`INSERT OR IGNORE INTO jobs_skipped
		 (id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
		 VALUES(?,?,?,?,?,?,?,'manual_reject',?,'',datetime('now'))`,
		jobID, userID, platform, company, role, location, link, score,
	); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review reject: failed to insert skipped job")
	}
	if _, err := h.svc.DB.ExecContext(r.Context(),
		"DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?", jobID, userID); err != nil {
		log.Error().Err(err).Str("job_id", jobID).Msg("review reject: failed to delete pending review")
	}

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
	res, err := h.svc.Bot.ApplyFromURL(r.Context(), userID, req.URL, req.Market, req.Force)
	if err != nil {
		if errors.Is(err, bot.ErrNotEasyApply) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "not_easy_apply",
				"message": "This job does not have Easy Apply / Quick Apply. It has been added to your Top Matches for manual application.",
				"company": res.Company,
				"role":    res.Role,
			})
			return
		}
		if errors.Is(err, bot.ErrAlreadyApplied) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "already_applied",
				"company": res.Company,
				"role":    res.Role,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if res.ScoreWarning {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "applied",
		"message": "Application submitted",
		"company": res.Company,
		"role":    res.Role,
	})
}
