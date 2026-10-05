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
	"github.com/user/jobifai/internal/llm"
)

type DocumentHandlers struct{ svc *Services }

func NewDocumentHandlers(svc *Services) *DocumentHandlers { return &DocumentHandlers{svc: svc} }

// GET /api/documents/versions/{version_id}
func (h *DocumentHandlers) GetVersion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	versionID := chi.URLParam(r, "version_id")
	out, err := h.svc.Documents.GetVersion(r.Context(), userID, versionID)
	if err != nil {
		if errors.Is(err, documents.ErrForbidden) || errors.Is(err, documents.ErrNotFound) {
			notFound(w, "document not found")
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/documents
func (h *DocumentHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.Documents == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "documents service unavailable"})
		return
	}
	out, err := h.svc.Documents.List(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type createResumeFromProfileReq struct {
	Title      string `json:"title"`
	Market     string `json:"market"`
	Style      string `json:"style"`
	DocumentID string `json:"document_id,omitempty"`
}

// POST /api/documents/resume/from-profile
func (h *DocumentHandlers) CreateResumeFromProfile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req createResumeFromProfileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	versionID, err := h.svc.Documents.CreateResumeFromProfile(r.Context(), userID, req.Title, documents.RenderContext{
		Market: req.Market, StyleName: req.Style, Language: "en",
	})
	writeDocumentVersionCreated(w, versionID, err)
}

type saveCoverReq struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Market     string `json:"market"`
	Style      string `json:"style"`
	DocumentID string `json:"document_id,omitempty"`
}

// POST /api/documents/cover-letter
func (h *DocumentHandlers) SaveCoverLetter(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req saveCoverReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	var versionID string
	var err error
	if req.DocumentID != "" {
		versionID, err = h.svc.Documents.SaveCoverLetterWithSourceOnDocument(r.Context(), userID, req.DocumentID, req.Title, req.Body, documents.SourceUserEdit, documents.RenderContext{
			Market: req.Market, StyleName: req.Style, Language: "en",
		})
	} else {
		versionID, err = h.svc.Documents.SaveCoverLetter(r.Context(), userID, req.Title, req.Body, documents.RenderContext{
			Market: req.Market, StyleName: req.Style, Language: "en",
		})
	}
	writeDocumentVersionCreated(w, versionID, err)
}

// POST /api/documents/cover-letter/ai-generate
func (h *DocumentHandlers) AIGenerateCoverLetter(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	_, tailor := h.svc.LLMFactory(userID)
	if tailor == nil {
		unprocessable(w, msgNoLLM)
		return
	}
	var req saveCoverReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	profile, err := h.loadProfileForDocs(userID)
	if err != nil || profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}
	prefix := h.docMarketPrefix(req.Market, "cover")
	ctx := llm.WithCallContext(r.Context(), domain.LLMCallContext{BypassReuse: true})
	body, err := tailor.WriteCoverLetter(ctx, profile, prefix+"Write a general cover letter suitable for open applications. No job description.\n")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	versionID, err := h.svc.Documents.SaveCoverLetterWithSource(r.Context(), userID, req.Title, body, documents.SourceAIGeneral, documents.RenderContext{
		Market: req.Market, StyleName: req.Style, Language: "en",
	})
	writeDocumentVersionCreated(w, versionID, err)
}

// POST /api/documents/resume/ai-improve
func (h *DocumentHandlers) AIImproveResume(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	_, tailor := h.svc.LLMFactory(userID)
	if tailor == nil {
		unprocessable(w, msgNoLLM)
		return
	}
	var req createResumeFromProfileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	profile, err := h.loadProfileForDocs(userID)
	if err != nil || profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}
	ctx := llm.WithCallContext(r.Context(), domain.LLMCallContext{BypassReuse: true})
	improved, err := tailor.TailorProfile(ctx, profile, h.docMarketPrefix(req.Market, "tailored")+"Improve wording for clarity and impact. No specific job description.\n")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	versionID, err := h.svc.Documents.SaveResumeVersion(r.Context(), userID, req.DocumentID, req.Title, documents.SourceAIImprove, improved, documents.RenderContext{
		Market: req.Market, StyleName: req.Style, Language: "en",
	})
	writeDocumentVersionCreated(w, versionID, err)
}

type setDefaultReq struct {
	Kind              string `json:"kind"`
	ContentVersionID  string `json:"content_version_id"`
}

type preferredStyleReq struct {
	Style string `json:"style"`
}

// PUT /api/documents/preferred-style
func (h *DocumentHandlers) SetPreferredStyle(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req preferredStyleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	if err := h.svc.Documents.NotePreferredStyle(r.Context(), userID, req.Style); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "preferred style noted")
}

// PUT /api/documents/defaults
func (h *DocumentHandlers) SetDefault(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req setDefaultReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	if err := h.svc.Documents.SetDefault(r.Context(), userID, req.Kind, req.ContentVersionID); err != nil {
		if errors.Is(err, documents.ErrForbidden) || errors.Is(err, documents.ErrNotFound) {
			notFound(w, err.Error())
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	if req.Kind == documents.KindResume {
		h.markResumeDefaultOnboarding(userID)
	}
	okMsg(w, "default updated")
}

func (h *DocumentHandlers) markResumeDefaultOnboarding(userID string) {
	var gs domain.GeneralSettings
	if err := h.svc.Config.Get(userID, keyGeneralSettings, &gs); err != nil {
		return
	}
	config.EnsureDocumentPolicies(&gs)
	if gs.DocumentPolicies.OnboardingComplete {
		return
	}
	gs.DocumentPolicies.OnboardingComplete = true
	_ = h.svc.Config.Set(userID, keyGeneralSettings, gs)
}

// GET /api/documents/versions/{version_id}/original
func (h *DocumentHandlers) DownloadOriginal(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	versionID := chi.URLParam(r, "version_id")
	data, filename, mediaType, err := h.svc.Documents.OriginalBytes(r.Context(), userID, versionID)
	if err != nil {
		if errors.Is(err, documents.ErrForbidden) || errors.Is(err, documents.ErrNotFound) {
			notFound(w, "original file not found")
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// GET /api/documents/versions/{version_id}/pdf
func (h *DocumentHandlers) DownloadPDF(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	versionID := chi.URLParam(r, "version_id")
	data, ctype, err := h.svc.Documents.PDFBytes(r.Context(), userID, versionID)
	if err != nil {
		if errors.Is(err, documents.ErrForbidden) || errors.Is(err, documents.ErrNotFound) {
			notFound(w, "document not found")
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", ctype)
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
	}
	w.Header().Set("Content-Disposition", disp+`; filename="document.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// POST /api/documents/originals
func (h *DocumentHandlers) UploadOriginal(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "file field required"})
		return
	}
	defer func() { _ = f.Close() }()
	mediaType := fh.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	versionID, err := h.svc.Documents.StoreOriginalUpload(r.Context(), userID, fh.Filename, mediaType, f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"content_version_id": versionID})
}

func (h *DocumentHandlers) loadProfileForDocs(userID string) (*domain.ResumeProfile, error) {
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, keyResumeProfile, &p); errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &p, nil
}

func (h *DocumentHandlers) docMarketPrefix(market, section string) string {
	if market == "" || h.svc.MarketDir == "" || h.svc.MarketPrefixLookup == nil {
		return ""
	}
	raw := h.svc.MarketPrefixLookup(h.svc.MarketDir, market, section)
	if raw == "" {
		return ""
	}
	return strings.TrimSpace(raw) + "\n\n"
}
