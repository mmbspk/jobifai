package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/jobpage"
)

func (h *ResumeHandlers) fetchJobPage(ctx context.Context, url string) (string, error) {
	if h.svc.FetchVerifiedJobPage != nil {
		return h.svc.FetchVerifiedJobPage(ctx, auth.UserIDFromCtx(ctx), url)
	}
	return h.svc.FetchJobPage(ctx, url)
}

func writeJobPageFailure(w http.ResponseWriter, err error) {
	code := jobpage.ErrorCode(err)
	if code == "" {
		writeURLUnreachable(w)
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": string(code), "message": "The job page could not be verified. Check the jobsite session or reader diagnostics before retrying."})
}

type AdminJobPageHandlers struct{ svc *Services }

func NewAdminJobPageHandlers(svc *Services) *AdminJobPageHandlers { return &AdminJobPageHandlers{svc} }
func (h *AdminJobPageHandlers) available(w http.ResponseWriter) bool {
	if h.svc.JobPages == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "Job page reading is unavailable"})
		return false
	}
	return true
}
func pageHTTPError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Could not update job page reading"
	if errors.Is(err, jobpage.ErrConflict) {
		status = http.StatusConflict
		message = "Settings changed. Reload and try again."
	}
	if errors.Is(err, jobpage.ErrForbidden) {
		status = http.StatusForbidden
		message = "Admin access required"
	}
	writeJSON(w, status, map[string]string{"message": message})
}
func (h *AdminJobPageHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	c, e := h.svc.JobPages.GetConfig(r.Context())
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
func (h *AdminJobPageHandlers) Set(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	var c jobpage.Config
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Invalid reader settings"})
		return
	}
	if _, e := jobpage.NormalizeConfig(c); e != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": e.Error()})
		return
	}
	updated, e := h.svc.JobPages.SetConfig(r.Context(), auth.UserIDFromCtx(r.Context()), c.Revision, c)
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
func (h *AdminJobPageHandlers) Recipes(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	state := jobpage.RecipeState(r.URL.Query().Get("state"))
	if state != "" && state != jobpage.RecipeActive && state != jobpage.RecipeInvalidated && state != jobpage.RecipeRetired {
		writeJSON(w, 400, map[string]string{"message": "Invalid recipe state"})
		return
	}
	entries, e := h.svc.JobPages.ListRecipes(r.Context(), state, 100)
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"recipes": entries})
}
func (h *AdminJobPageHandlers) Retire(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	e := h.svc.JobPages.RetireRecipe(r.Context(), auth.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, 200, map[string]string{"message": "Recipe retired"})
}
func (h *AdminJobPageHandlers) Audit(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	entries, e := h.svc.JobPages.ListAudit(r.Context(), 100)
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"entries": entries})
}
func (h *AdminJobPageHandlers) Events(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	entries, e := h.svc.JobPages.ListReadEvents(r.Context(), r.URL.Query().Get("user_id"), 100)
	if e != nil {
		pageHTTPError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"events": entries})
}

// Canary opens only the signed-in operator's listing session and calls the
// same worker reader as discovery. It has no path to apply or document work.
func (h *AdminJobPageHandlers) Canary(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if h.svc.ReadJobPage == nil {
		writeJSON(w, 503, map[string]string{"message": "Worker page reader unavailable"})
		return
	}
	var request struct {
		URL string `json:"url"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if e := d.Decode(&request); e != nil {
		writeJSON(w, 400, map[string]string{"message": "Invalid listing URL"})
		return
	}
	if _, _, e := jobpage.NormalizeTarget(jobpage.Target{URL: request.URL}); e != nil {
		writeJSON(w, 400, map[string]string{"message": "Use a LinkedIn or SEEK listing URL"})
		return
	}
	result, e := h.svc.ReadJobPage(r.Context(), auth.UserIDFromCtx(r.Context()), request.URL)
	response := map[string]any{"result": result, "description_characters": utf8.RuneCountInString(result.Description), "failure_code": jobpage.ErrorCode(e), "success": e == nil}
	if e != nil {
		response["message"] = "The worker could not verify a full description. Check the recorded read result."
	}
	writeJSON(w, 200, response)
}
