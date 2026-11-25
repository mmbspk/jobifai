package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
)

// ApplyTask persists an operation independently of the requesting page.
type ApplyTask struct {
	ID         string          `json:"id"`
	URL        string          `json:"url"`
	Market     string          `json:"market"`
	Force      bool            `json:"force"`
	State      string          `json:"state"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  string          `json:"started_at"`
	FinishedAt string          `json:"finished_at,omitempty"`
}

func scanApplyTask(row *sql.Row) (ApplyTask, error) {
	var t ApplyTask
	var response string
	err := row.Scan(&t.ID, &t.URL, &t.Market, &t.Force, &t.State, &response, &t.Error, &t.StartedAt, &t.FinishedAt)
	if response != "" {
		t.Response = json.RawMessage(response)
	}
	return t, err
}

const applyTaskColumns = "id,url,market,force,state,response_json,error,started_at,finished_at"

func (h *BotHandlers) applyTask(ctx context.Context, userID, id string) (ApplyTask, error) {
	return scanApplyTask(h.svc.DB.QueryRowContext(ctx, "SELECT "+applyTaskColumns+" FROM apply_url_tasks WHERE user_id=? AND id=?", userID, id))
}

func validApplyTaskURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	seek := host == "seek.com" || strings.HasSuffix(host, ".seek.com") || host == "seek.com.au" || strings.HasSuffix(host, ".seek.com.au")
	linkedin := host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com")
	return (seek && strings.HasPrefix(u.Path, "/job/")) || (linkedin && strings.HasPrefix(u.Path, "/jobs/"))
}

// StartApplyTask returns as soon as work is durably claimed. The client's ID
// makes a lost acknowledgement safe to retry without starting another application.
func (h *BotHandlers) StartApplyTask(w http.ResponseWriter, r *http.Request) {
	if h.svc.Bot == nil || h.svc.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": msgBotNotInit})
		return
	}
	var req struct {
		ID     string `json:"id"`
		URL    string `json:"url"`
		Market string `json:"market"`
		Force  bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if _, err := uuid.Parse(req.ID); err != nil || !validApplyTaskURL(req.URL) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "a request ID and direct LinkedIn or SEEK job URL are required"})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	defer h.svc.lockBrowserWork(userID)()
	h.applyMu.Lock()
	defer h.applyMu.Unlock()
	prior, err := h.applyTask(r.Context(), userID, req.ID)
	if err == nil {
		if prior.URL != req.URL || prior.Market != req.Market || prior.Force != req.Force {
			conflict(w, "request ID already belongs to a different application")
			return
		}
		writeJSON(w, http.StatusOK, prior)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not read AI Apply operation"})
		return
	}
	platform := "seek"
	if u, _ := url.Parse(req.URL); strings.Contains(u.Hostname(), "linkedin.com") {
		platform = "linkedin"
	}
	if h.svc.BrowserMgr != nil {
		if _, pending := h.svc.BrowserMgr.PendingSession(userID, platform); pending {
			conflict(w, "Finish signing in and save the open job-site session before starting AI Apply.")
			return
		}
	}
	t := ApplyTask{ID: req.ID, URL: req.URL, Market: req.Market, Force: req.Force, State: "running", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_, err = h.svc.DB.ExecContext(r.Context(), "INSERT INTO apply_url_tasks(id,user_id,url,market,force,state,started_at) VALUES(?,?,?,?,?,'running',?)",
		t.ID, userID, t.URL, t.Market, t.Force, t.StartedAt)
	if err != nil {
		conflict(w, "Another AI Apply operation is active. Return to it before starting another.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	h.applyCancels[t.ID] = cancel
	go h.runApplyTask(ctx, userID, t, cancel)
	writeJSON(w, http.StatusAccepted, t)
}

func (h *BotHandlers) runApplyTask(ctx context.Context, userID string, t ApplyTask, cancel context.CancelFunc) {
	defer cancel()
	// HTTP recovery middleware cannot catch a panic in this detached worker.
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error().Str("task_id", t.ID).Msg("AI Apply worker stopped unexpectedly")
			_, err := h.svc.DB.Exec("UPDATE apply_url_tasks SET state='failed',error=?,finished_at=? WHERE id=? AND user_id=? AND state='running'",
				"AI Apply stopped unexpectedly. Check the job site's application status before retrying.", time.Now().UTC().Format(time.RFC3339Nano), t.ID, userID)
			if err != nil {
				log.Error().Err(err).Str("task_id", t.ID).Msg("AI Apply failure persistence failed")
			}
		}
	}()
	defer func() { h.applyMu.Lock(); delete(h.applyCancels, t.ID); h.applyMu.Unlock() }()
	res, err := h.svc.Bot.ApplyFromURL(ctx, userID, t.URL, t.Market, t.Force)
	response := map[string]any{"company": res.Company, "role": res.Role, "job_id": res.JobID}
	state, message := "succeeded", ""
	switch {
	case errors.Is(err, bot.ErrAlreadyApplied):
		response["status"] = "already_applied"
	case errors.Is(err, bot.ErrNotEasyApply):
		response["status"] = "not_easy_apply"
		response["message"] = "This job requires a manual application and is in Top Matches."
	case err != nil:
		state, message = "failed", err.Error()
		if errors.Is(ctx.Err(), context.Canceled) {
			state = "cancelled"
			message = "AI Apply cancelled. Check the job site's application status before retrying."
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message = "AI Apply timed out. Check the job site's application status before retrying."
		}
	case res.ScoreWarning:
		response["status"], response["score"], response["reasoning"] = "score_warning", res.Score, res.ScoreReason
	default:
		response["status"], response["message"] = "applied", "Application submitted"
	}
	encoded, _ := json.Marshal(response)
	if state != "succeeded" {
		encoded = nil
	}
	tx, saveErr := h.svc.DB.Begin()
	if saveErr != nil {
		log.Error().Err(saveErr).Str("task_id", t.ID).Msg("AI Apply result transaction failed")
		return
	}
	defer func() { _ = tx.Rollback() }()
	updated, saveErr := tx.Exec("UPDATE apply_url_tasks SET state=?,response_json=?,error=?,finished_at=? WHERE user_id=? AND id=? AND state='running'",
		state, string(encoded), message, time.Now().UTC().Format(time.RFC3339Nano), userID, t.ID)
	if saveErr != nil {
		log.Error().Err(saveErr).Str("task_id", t.ID).Msg("AI Apply result persistence failed")
		return
	}
	count, saveErr := updated.RowsAffected()
	if saveErr != nil || count != 1 {
		return
	}
	if state == "succeeded" {
		// Only a completed, classified attempt replaces the old Cannot Apply diagnostic.
		// Opening the retry form or a failed/interrupted run preserves the listing.
		if _, cleanupErr := tx.Exec("DELETE FROM jobs_skipped WHERE user_id=? AND link=? AND "+sqlCannotApplyFilter, userID, t.URL); cleanupErr != nil {
			log.Error().Err(cleanupErr).Str("task_id", t.ID).Msg("AI Apply diagnostic cleanup failed")
			return
		}
		if _, cleanupErr := tx.Exec("DELETE FROM jobs_approved_queue WHERE user_id=? AND link=?", userID, t.URL); cleanupErr != nil {
			log.Error().Err(cleanupErr).Str("task_id", t.ID).Msg("AI Apply retry queue cleanup failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("AI Apply result commit failed")
	}
}

func (h *BotHandlers) GetApplyTask(w http.ResponseWriter, r *http.Request) {
	t, err := h.applyTask(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "task_id"))
	if errors.Is(err, sql.ErrNoRows) {
		notFound(w, "AI Apply operation not found")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not read AI Apply operation"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *BotHandlers) CurrentApplyTask(w http.ResponseWriter, r *http.Request) {
	t, err := scanApplyTask(h.svc.DB.QueryRowContext(r.Context(), "SELECT "+applyTaskColumns+" FROM apply_url_tasks WHERE user_id=? ORDER BY started_at DESC LIMIT 1", auth.UserIDFromCtx(r.Context())))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not read AI Apply operation"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": t})
}

func (h *BotHandlers) CancelApplyTask(w http.ResponseWriter, r *http.Request) {
	userID, id := auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "task_id")
	if _, err := h.applyTask(r.Context(), userID, id); err != nil {
		notFound(w, "AI Apply operation not found")
		return
	}
	h.applyMu.Lock()
	if cancel := h.applyCancels[id]; cancel != nil {
		cancel()
	}
	h.applyMu.Unlock()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Cancellation requested"})
}
