package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/documents"
)

// DELETE /api/documents/{document_id}
func (h *DocumentHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	if h.svc.Documents == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "documents service unavailable"})
		return
	}
	err := h.svc.Documents.DeleteDocument(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "document_id"))
	if errors.Is(err, documents.ErrNotFound) || errors.Is(err, documents.ErrForbidden) {
		notFound(w, "document not found")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not delete document"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "document deleted"})
}
