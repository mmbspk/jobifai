package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
)

const (
	msgNoRenderer   = "PDF renderer not available"
	msgNoProfile    = "no resume profile found, save one at /api/settings/resume first"
	msgBadMultipart = "invalid multipart form"
	msgNoLLM        = "LLM not configured, set an API key in Settings → Secrets first"
)

func writeURLUnreachable(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
		"code":    "url_unreachable",
		"message": "Could not fetch job details from the provided URL. The page may require login or be unavailable.",
	})
}

// ResumeHandlers groups resume generation handlers.
type ResumeHandlers struct{ svc *Services }

func NewResumeHandlers(svc *Services) *ResumeHandlers { return &ResumeHandlers{svc: svc} }

func writePDF(w http.ResponseWriter, filename string, data []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// loadProfile loads the stored resume profile for userID, returning nil if none is saved.
func (h *ResumeHandlers) loadProfile(userID string) (*domain.ResumeProfile, error) {
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, "resume_profile", &p); errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &p, nil
}

// extraContext builds an optional context string from supplemental form fields.
func extraContext(r *http.Request) string {
	var parts []string
	if v := strings.TrimSpace(r.FormValue("linkedin_url")); v != "" {
		parts = append(parts, "LinkedIn: "+v)
	}
	if v := strings.TrimSpace(r.FormValue("github_url")); v != "" {
		parts = append(parts, "GitHub: "+v)
	}
	if v := strings.TrimSpace(r.FormValue("prompt_hint")); v != "" {
		parts = append(parts, v)
	}
	return strings.Join(parts, "\n")
}

// marketPrefix returns the market-specific prompt prefix to prepend to LLM calls.
// It reads the market name from the "market" form field, loads the YAML, and
// returns the appropriate prompt section. Returns empty string if not found.
func (h *ResumeHandlers) marketPrefix(r *http.Request, section string) string {
	market := strings.TrimSpace(r.FormValue("market"))
	if market == "" || h.svc.MarketDir == "" || h.svc.MarketPrefixLookup == nil {
		return ""
	}
	raw := h.svc.MarketPrefixLookup(h.svc.MarketDir, market, section)
	if raw == "" {
		return ""
	}
	return strings.TrimSpace(raw) + "\n\n"
}

// marketCSSFile returns the CSS file path for the selected market, or empty string.
func (h *ResumeHandlers) marketCSSFile(r *http.Request) string {
	market := strings.TrimSpace(r.FormValue("market"))
	if market == "" || h.svc.MarketDir == "" || h.svc.MarketCSSFileLookup == nil {
		return ""
	}
	return h.svc.MarketCSSFileLookup(h.svc.MarketDir, market)
}

// POST /api/resume/generate
func (h *ResumeHandlers) Generate(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.Renderer == nil {
		unprocessable(w, msgNoRenderer)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}

	profile, err := h.loadProfile(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	// Optional: override profile with uploaded file (uses LLM if configured)
	if f, fh, ferr := r.FormFile("resume_file"); ferr == nil {
		defer f.Close()
		extractor, _ := h.svc.LLMFactory(userID)
		if extractor != nil {
			text, _ := h.svc.FileToText(f, fh.Filename)
			if extracted, llmErr := extractor.ExtractFromText(r.Context(), text); llmErr == nil {
				profile = extracted
			}
		} else {
			_, _ = io.Copy(io.Discard, f)
		}
	}

	if profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}

	// If market or extra context supplied, tailor the profile before rendering
	marketCtx := h.marketPrefix(r, "resume")
	extraCtx := extraContext(r)
	if combinedCtx := marketCtx + extraCtx; combinedCtx != "" {
		if _, tailor := h.svc.LLMFactory(userID); tailor != nil {
			if tailored, err := tailor.TailorProfile(r.Context(), profile, combinedCtx); err == nil {
				profile = tailored
			}
		}
	}

	pdf, err := h.svc.Renderer.RenderResume(r.Context(), profile, r.FormValue("style"), h.marketCSSFile(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writePDF(w, "resume.pdf", pdf)
}

// POST /api/resume/generate-tailored
func (h *ResumeHandlers) GenerateTailored(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.Renderer == nil {
		unprocessable(w, msgNoRenderer)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}

	jobURL := strings.TrimSpace(r.FormValue("job_url"))
	jobDesc := strings.TrimSpace(r.FormValue("job_description"))
	skipFetch := r.FormValue("skip_url_fetch") == "true"

	if jobURL == "" && jobDesc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "job_url or job_description is required"})
		return
	}

	_, tailor := h.svc.LLMFactory(userID)
	if tailor == nil {
		unprocessable(w, msgNoLLM)
		return
	}

	// Try to fetch job page content if no manual description provided
	if jobDesc == "" && jobURL != "" && !skipFetch {
		fetched, fetchErr := h.svc.FetchJobPage(r.Context(), jobURL)
		if fetchErr != nil {
			writeURLUnreachable(w)
			return
		}
		jobDesc = fetched
	}

	profile, err := h.loadProfile(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}

	jobContext := h.marketPrefix(r, "tailored")
	if jobURL != "" {
		jobContext += "Job URL: " + jobURL + "\n"
	}
	if jobDesc != "" {
		jobContext += "Job Description:\n" + jobDesc + "\n"
	}
	jobContext += extraContext(r)

	tailored, err := tailor.TailorProfile(r.Context(), profile, jobContext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	pdf, err := h.svc.Renderer.RenderResume(r.Context(), tailored, r.FormValue("style"), h.marketCSSFile(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writePDF(w, "resume_tailored.pdf", pdf)
}

// POST /api/resume/evaluate
func (h *ResumeHandlers) EvaluateJob(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}

	jobURL := strings.TrimSpace(r.FormValue("job_url"))
	jobDesc := strings.TrimSpace(r.FormValue("job_description"))
	skipFetch := r.FormValue("skip_url_fetch") == "true"

	if jobURL == "" && jobDesc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "job_url or job_description is required"})
		return
	}

	if h.svc.EvaluatorFactory == nil {
		unprocessable(w, msgNoLLM)
		return
	}
	evaluator := h.svc.EvaluatorFactory(userID)
	if evaluator == nil {
		unprocessable(w, msgNoLLM)
		return
	}

	if jobDesc == "" && jobURL != "" && !skipFetch {
		fetched, err := h.svc.FetchJobPage(r.Context(), jobURL)
		if err != nil {
			writeURLUnreachable(w)
			return
		}
		jobDesc = fetched
	}

	profile, err := h.loadProfile(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}

	result, err := evaluator.EvaluateJob(r.Context(), profile, jobDesc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *ResumeHandlers) GenerateCoverLetter(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.Renderer == nil {
		unprocessable(w, msgNoRenderer)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}

	jobURL := strings.TrimSpace(r.FormValue("job_url"))
	jobDesc := strings.TrimSpace(r.FormValue("job_description"))
	skipFetch := r.FormValue("skip_url_fetch") == "true"

	_, tailor := h.svc.LLMFactory(userID)
	if tailor == nil {
		unprocessable(w, msgNoLLM)
		return
	}

	// Try to fetch job page content if no manual description provided
	if jobDesc == "" && jobURL != "" && !skipFetch {
		fetched, fetchErr := h.svc.FetchJobPage(r.Context(), jobURL)
		if fetchErr != nil {
			writeURLUnreachable(w)
			return
		}
		jobDesc = fetched
	}

	profile, err := h.loadProfile(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}

	jobContext := h.marketPrefix(r, "cover")
	if jobURL != "" {
		jobContext += "Job URL: " + jobURL + "\n"
	}
	if jobDesc != "" {
		jobContext += "Job Description:\n" + jobDesc + "\n"
	}
	jobContext += extraContext(r)

	body, err := tailor.WriteCoverLetter(r.Context(), profile, jobContext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	pdf, err := h.svc.Renderer.RenderCoverLetter(r.Context(), body, r.FormValue("style"), h.marketCSSFile(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writePDF(w, "cover_letter.pdf", pdf)
}

// POST /api/resume/check-halal
func (h *ResumeHandlers) CheckHalal(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": msgBadMultipart})
		return
	}

	jobURL := strings.TrimSpace(r.FormValue("job_url"))
	jobDesc := strings.TrimSpace(r.FormValue("job_description"))
	skipFetch := r.FormValue("skip_url_fetch") == "true"
	title := strings.TrimSpace(r.FormValue("title"))
	company := strings.TrimSpace(r.FormValue("company"))

	if jobURL == "" && jobDesc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "job_url or job_description is required"})
		return
	}

	if h.svc.HalalCheckerFactory == nil {
		unprocessable(w, msgNoLLM)
		return
	}
	checker := h.svc.HalalCheckerFactory(userID)
	if checker == nil {
		unprocessable(w, msgNoLLM)
		return
	}

	if jobDesc == "" && jobURL != "" && !skipFetch {
		fetched, err := h.svc.FetchJobPage(r.Context(), jobURL)
		if err != nil {
			writeURLUnreachable(w)
			return
		}
		jobDesc = fetched
	}

	verdict, err := checker.CheckHalal(r.Context(), title, company, jobDesc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, verdict)
}

// POST /api/resume/answer-questions
func (h *ResumeHandlers) AnswerQuestions(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var req domain.AnswerQuestionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON body"})
		return
	}

	var nonEmpty []string
	for _, q := range req.Questions {
		if s := strings.TrimSpace(q); s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	if len(nonEmpty) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "at least one question is required"})
		return
	}
	if req.JobURL == "" && req.JobDesc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "job_url or job_description is required"})
		return
	}

	if h.svc.QuestionAnswererFactory == nil {
		unprocessable(w, msgNoLLM)
		return
	}
	answerer := h.svc.QuestionAnswererFactory(userID)
	if answerer == nil {
		unprocessable(w, msgNoLLM)
		return
	}

	jobDesc := req.JobDesc
	if jobDesc == "" && req.JobURL != "" {
		fetched, err := h.svc.FetchJobPage(r.Context(), req.JobURL)
		if err != nil {
			writeURLUnreachable(w)
			return
		}
		jobDesc = fetched
	}

	profile, err := h.loadProfile(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if profile == nil {
		unprocessable(w, msgNoProfile)
		return
	}

	var jobContext strings.Builder
	if req.JobURL != "" {
		jobContext.WriteString("Job URL: " + req.JobURL + "\n")
	}
	if jobDesc != "" {
		jobContext.WriteString("Job Description:\n" + jobDesc + "\n")
	}

	answers, err := answerer.AnswerQuestions(r.Context(), profile, jobContext.String(), nonEmpty)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, answers)
}
