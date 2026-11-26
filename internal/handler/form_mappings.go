package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
)

func (h *SettingsHandlers) FormMappings(w http.ResponseWriter, r *http.Request) {
	items, err := bot.ListFormMappingReviews(r.Context(), h.svc.DB, auth.UserIDFromCtx(r.Context()))
	if err != nil {
		writeJSON(w, 500, map[string]string{"message": "could not load form mappings"})
		return
	}
	writeJSON(w, 200, map[string]any{"entries": items})
}

func (h *SettingsHandlers) ConfirmFormMapping(w http.ResponseWriter, r *http.Request) {
	userID, id := auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "id")
	var request struct {
		Confirmed  bool   `json:"confirmed"`
		BindingKey string `json:"binding_key"`
		Answer     string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil || !request.Confirmed {
		writeJSON(w, 400, map[string]string{"message": "explicit question and answer confirmation is required"})
		return
	}
	var found int
	if err := h.svc.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM form_field_mappings WHERE id=? AND user_id=? AND schema_version=4`, id, userID).Scan(&found); err != nil {
		writeJSON(w, 500, map[string]string{"message": "could not load form mapping"})
		return
	}
	if found != 1 {
		writeJSON(w, 404, map[string]string{"message": "mapping not found"})
		return
	}
	configTx, ok := h.svc.Config.(TransactionalConfigStore)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "transactional profile updates are unavailable"})
		return
	}
	tx, err := h.svc.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not begin question review"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	var profile domain.ResumeProfile
	if err := configTx.GetTx(r.Context(), tx, userID, keyResumeProfile, &profile); err != nil {
		writeJSON(w, 400, map[string]string{"message": "save your profile before reviewing form answers"})
		return
	}
	binding := strings.TrimSpace(request.BindingKey)
	if binding == "" {
		answer := strings.TrimSpace(request.Answer)
		if answer == "" || len(answer) > 2000 {
			writeJSON(w, 400, map[string]string{"message": "provide a reviewed answer of 1–2000 characters"})
			return
		}
		key := "mapping_" + id
		if profile.ApplicationDefaults.FormAnswers == nil {
			profile.ApplicationDefaults.FormAnswers = map[string]string{}
		}
		profile.ApplicationDefaults.FormAnswers[key] = answer
		binding = "application_defaults.form_answers." + key
		if err := configTx.SetTx(r.Context(), tx, userID, keyResumeProfile, profile); err != nil {
			writeJSON(w, 500, map[string]string{"message": "could not save candidate answer"})
			return
		}
	}
	if err := bot.ConfirmFormMappingTx(r.Context(), tx, userID, id, binding, &profile); err != nil {
		writeJSON(w, 400, map[string]string{"message": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not save question review"})
		return
	}
	okMsg(w, "question mapping confirmed")
}

func (h *SettingsHandlers) InvalidateFormMapping(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.DB.ExecContext(r.Context(), "UPDATE form_field_mappings SET provenance='invalidated',updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?", chi.URLParam(r, "id"), auth.UserIDFromCtx(r.Context()))
	if err != nil {
		writeJSON(w, 500, map[string]string{"message": "could not invalidate mapping"})
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeJSON(w, 404, map[string]string{"message": "mapping not found"})
		return
	}
	okMsg(w, "question mapping invalidated")
}
