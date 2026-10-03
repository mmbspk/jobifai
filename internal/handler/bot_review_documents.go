package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

type reviewDocumentsResponse struct {
	Options   documents.ReviewDocumentOptions   `json:"options"`
	Preflight documents.ReviewDocumentPreflight `json:"preflight"`
}

type reviewDocumentsSelectionRequest struct {
	ResumeVersionID string `json:"resume_version_id"`
	CoverVersionID  string `json:"cover_version_id"`
	ResumeUseSite   bool   `json:"resume_use_site"`
	CoverSkip       bool   `json:"cover_skip"`
}

func (h *BotHandlers) reviewPendingRow(r *http.Request, userID, jobID string) (
	resumeCol, coverCol, refsJSON string, err error,
) {
	err = h.svc.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(resume_content_version_id,''), COALESCE(cover_letter_content_version_id,''), COALESCE(document_refs_json,'')
		 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, jobID, userID,
	).Scan(&resumeCol, &coverCol, &refsJSON)
	return
}

func (h *BotHandlers) reviewResolveContext(r *http.Request, userID string, resumeCol, coverCol, refsJSON string) (
	documents.ResolveReviewInput, documents.ReviewDocumentOptions, error,
) {
	pack := documents.ParseApplicationPackJSON(refsJSON)
	sel := documents.SelectionFromPack(pack, resumeCol, coverCol)
	caps := pack.FormCaps

	var gs domain.GeneralSettings
	if err := h.svc.Config.Get(userID, "general_settings", &gs); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return documents.ResolveReviewInput{}, documents.ReviewDocumentOptions{}, err
	}
	config.EnsureDocumentPolicies(&gs)

	var def documents.DefaultsView
	if h.svc.Documents != nil {
		if list, err := h.svc.Documents.List(r.Context(), userID); err == nil {
			def = list.Defaults
		}
	}
	effectiveResume := def.ResumeVersionID
	if gs.DocumentPolicies.RegionalDefaults != nil {
		if vid := strings.TrimSpace(gs.DocumentPolicies.RegionalDefaults[strings.TrimSpace(gs.DefaultResumeMarket)]); vid != "" {
			effectiveResume = vid
		}
	}

	hasProfile := false
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, "resume_profile", &p); err == nil && strings.TrimSpace(p.PersonalInformation.Name) != "" {
		hasProfile = true
	}
	hasResumeDefault := effectiveResume != "" || def.ResumeVersionID != ""
	hasCoverDefault := def.CoverLetterVersionID != ""

	opts := documents.ReviewDocumentOptions{
		Resume: []documents.ReviewDocumentChoice{},
		Cover:  []documents.ReviewDocumentChoice{},
	}
	if h.svc.Documents != nil {
		if list, err := h.svc.Documents.List(r.Context(), userID); err == nil {
			opts = documents.BuildReviewDocumentOptions(list)
		}
	}

	in := documents.ResolveReviewInput{
		Policies:                 gs.DocumentPolicies,
		Defaults:                 def,
		EffectiveResumeVersionID: effectiveResume,
		Selection:                sel,
		Caps:                     caps,
		Pack:                     pack,
		HasConfirmedProfile:      hasProfile,
		DefaultResumeExists:      hasResumeDefault,
		DefaultCoverExists:       hasCoverDefault,
	}
	return in, opts, nil
}

// GET /api/bot/review/{job_id}/documents
func (h *BotHandlers) ReviewGetDocuments(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")
	resumeCol, coverCol, refsJSON, err := h.reviewPendingRow(r, userID, jobID)
	if err != nil {
		notFound(w, "no pending review for job_id "+jobID)
		return
	}
	in, opts, err := h.reviewResolveContext(r, userID, resumeCol, coverCol, refsJSON)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	pf, _ := documents.ResolveReviewPreflight(in)
	writeJSON(w, http.StatusOK, reviewDocumentsResponse{Options: opts, Preflight: pf})
}

// PUT /api/bot/review/{job_id}/documents
func (h *BotHandlers) ReviewPutDocuments(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "job_id")
	if h.svc.Documents == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "documents service unavailable"})
		return
	}
	resumeCol, coverCol, refsJSON, err := h.reviewPendingRow(r, userID, jobID)
	if err != nil {
		notFound(w, "no pending review for job_id "+jobID)
		return
	}
	var req reviewDocumentsSelectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON body"})
		return
	}
	req.ResumeVersionID = strings.TrimSpace(req.ResumeVersionID)
	req.CoverVersionID = strings.TrimSpace(req.CoverVersionID)
	if req.ResumeUseSite && req.ResumeVersionID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "choose either a saved resume or job-site resume, not both"})
		return
	}
	if req.CoverSkip && req.CoverVersionID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "choose either a cover letter version or skip cover, not both"})
		return
	}
	if err := h.svc.Documents.ValidateUserVersionKind(r.Context(), userID, req.ResumeVersionID, documents.KindResume); err != nil {
		if errors.Is(err, documents.ErrReviewVersionWrongKind) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "resume version not found"})
		return
	}
	if err := h.svc.Documents.ValidateUserVersionKind(r.Context(), userID, req.CoverVersionID, documents.KindCoverLetter); err != nil {
		if errors.Is(err, documents.ErrReviewVersionWrongKind) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "cover letter version not found"})
		return
	}

	sel := documents.ApplicationDocumentSelection{
		ResumeVersionID: req.ResumeVersionID,
		CoverVersionID:  req.CoverVersionID,
		ResumeUseSite:   req.ResumeUseSite,
		CoverSkip:       req.CoverSkip,
	}
	pack := documents.PackAfterSelectionChange(sel)
	packJSON := documents.WriteApplicationPackJSON(pack)
	if packJSON == "" {
		packJSON = documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{Selection: sel, HoldReason: pack.HoldReason})
	}
	_, err = h.svc.DB.ExecContext(r.Context(),
		`UPDATE jobs_pending_review
		 SET resume_content_version_id = ?, cover_letter_content_version_id = ?,
		     resume_path = '', cover_letter_path = '', document_refs_json = ?
		 WHERE job_id = ? AND user_id = ?`,
		sel.ResumeVersionID, sel.CoverVersionID, packJSON, jobID, userID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	_ = resumeCol
	_ = coverCol
	_ = refsJSON
	in, opts, err := h.reviewResolveContext(r, userID, sel.ResumeVersionID, sel.CoverVersionID, packJSON)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	pf, _ := documents.ResolveReviewPreflight(in)
	writeJSON(w, http.StatusOK, reviewDocumentsResponse{Options: opts, Preflight: pf})
}
