package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// Keep the Generate endpoints' retry-safe billing identity while the document
// service owns the single render, storage, and artifact-linking operation.
type generationRenderBiller struct {
	documents.RenderBiller
	operationID string
}

func (b generationRenderBiller) RecordRender(ctx context.Context, userID, _ string) error {
	return b.RenderBiller.RecordRender(ctx, userID, b.operationID)
}

func (h *ResumeHandlers) generationDocuments(r *http.Request, operation string) *documents.Service {
	// A request-local copy avoids changing the shared service's biller.
	svc := *h.svc.Documents
	biller := svc.Biller
	if h.svc.Resource != nil {
		biller = h.svc.Resource
	}
	if biller != nil {
		svc.Biller = generationRenderBiller{
			RenderBiller: biller,
			operationID:  legacyOpID(r, auth.UserIDFromCtx(r.Context()), operation),
		}
	}
	return &svc
}

func generatedRenderContext(r *http.Request) documents.RenderContext {
	return documents.RenderContext{Market: r.FormValue("market"), StyleName: r.FormValue("style"), Language: "en"}
}

func (h *ResumeHandlers) saveGeneratedResume(w http.ResponseWriter, r *http.Request, profile *domain.ResumeProfile, source, title, operation, filename string) {
	svc := h.generationDocuments(r, operation)
	userID := auth.UserIDFromCtx(r.Context())
	versionID, err := svc.SaveResumeVersion(r.Context(), userID, "", title, source, profile, generatedRenderContext(r))
	h.writeGeneratedDocument(w, r, svc, versionID, filename, err)
}

func (h *ResumeHandlers) saveGeneratedCover(w http.ResponseWriter, r *http.Request, body string) {
	svc := h.generationDocuments(r, "cover-generate")
	userID := auth.UserIDFromCtx(r.Context())
	versionID, err := svc.SaveCoverLetterWithSource(r.Context(), userID, "Generated cover letter", body, documents.SourceAIGeneral, generatedRenderContext(r))
	h.writeGeneratedDocument(w, r, svc, versionID, "cover_letter.pdf", err)
}

func (h *ResumeHandlers) writeGeneratedDocument(w http.ResponseWriter, r *http.Request, svc *documents.Service, versionID, filename string, err error) {
	if errors.Is(err, quota.ErrExceeded) {
		writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits for document render"})
		return
	}
	if versionID != "" {
		w.Header().Set("X-Jobifai-Document-Version", versionID)
	}
	if err != nil {
		if versionID != "" {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"message":            "Content saved to Documents, but PDF creation failed: " + err.Error(),
				"content_version_id": versionID,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	pdf, _, _, err := svc.PDFBytes(r.Context(), auth.UserIDFromCtx(r.Context()), versionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Saved to Documents, but PDF download failed: " + err.Error()})
		return
	}
	writePDF(w, filename, pdf)
}
