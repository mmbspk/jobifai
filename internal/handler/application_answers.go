package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
)

type ApplicationAnswerHandlers struct{ svc *Services }

func NewApplicationAnswerHandlers(svc *Services) *ApplicationAnswerHandlers {
	return &ApplicationAnswerHandlers{svc: svc}
}

func answerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrApplicationAnswerConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
	case errors.Is(err, domain.ErrNotFound):
		notFound(w, "answer review not found")
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
	}
}

func (h *ApplicationAnswerHandlers) available(w http.ResponseWriter) bool {
	if h.svc.ApplicationAnswers != nil {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "application answers unavailable"})
	return false
}

func (h *ApplicationAnswerHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	out, err := h.svc.ApplicationAnswers.Get(r.Context(), auth.UserIDFromCtx(r.Context()))
	if err != nil {
		answerError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *ApplicationAnswerHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	var request domain.ApplicationAnswersPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"message": "invalid answer update"})
		return
	}
	out, err := h.svc.ApplicationAnswers.Patch(r.Context(), auth.UserIDFromCtx(r.Context()), request)
	if err != nil {
		answerError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *ApplicationAnswerHandlers) List(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.svc.ApplicationAnswers.ListReviews(r.Context(), auth.UserIDFromCtx(r.Context()), r.URL.Query().Get("cursor"), r.URL.Query().Get("state"), limit)
	if err != nil {
		answerError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *ApplicationAnswerHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	out, err := h.svc.ApplicationAnswers.GetReview(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		answerError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *ApplicationAnswerHandlers) Confirm(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	var request domain.ApplicationAnswerConfirmation
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"message": "invalid answer confirmation"})
		return
	}
	out, err := h.svc.ApplicationAnswers.Confirm(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), request)
	if err != nil {
		answerError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"value": out.ApplicationDefaults, "revision": out.Revision})
}
func (h *ApplicationAnswerHandlers) Dismiss(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	var request struct {
		Revision int64 `json:"revision"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"message": "invalid review dismissal"})
		return
	}
	if err := h.svc.ApplicationAnswers.Dismiss(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), request.Revision); err != nil {
		answerError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *ApplicationAnswerHandlers) Read(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if err := h.svc.ApplicationAnswers.MarkRead(r.Context(), auth.UserIDFromCtx(r.Context())); err != nil {
		answerError(w, err)
		return
	}
	w.WriteHeader(204)
}
