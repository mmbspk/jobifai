package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

type appendResumeReq struct {
	Title   string                `json:"title"`
	Market  string                `json:"market"`
	Style   string                `json:"style"`
	Profile domain.ResumeProfile  `json:"profile"`
}

// POST /api/documents/{document_id}/resume-versions
func (h *DocumentHandlers) AppendResumeVersion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	documentID := chi.URLParam(r, "document_id")
	var req appendResumeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	versionID, err := h.svc.Documents.AppendResumeVersion(r.Context(), userID, documentID, req.Title, documents.SourceUserEdit, &req.Profile, documents.RenderContext{
		Market: req.Market, StyleName: req.Style, Language: "en",
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"content_version_id": versionID})
}

// POST /api/documents/{document_id}/cover-versions
func (h *DocumentHandlers) AppendCoverVersion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	documentID := chi.URLParam(r, "document_id")
	var req saveCoverReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	versionID, err := h.svc.Documents.AppendCoverVersion(r.Context(), userID, documentID, req.Title, req.Body, documents.SourceUserEdit, documents.RenderContext{
		Market: req.Market, StyleName: req.Style, Language: "en",
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"content_version_id": versionID})
}
