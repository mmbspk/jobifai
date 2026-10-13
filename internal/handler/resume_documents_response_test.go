package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/quota"
)

func TestGeneratedDocument_QuotaFailureDoesNotAdvertiseSavedVersion(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/resume/generate", nil)
	h := &ResumeHandlers{}
	// Even if a save operation returns an ID alongside quota failure, 402 must
	// not advertise that ID as a successfully generated document.
	h.writeGeneratedDocument(w, r, nil, "saved-version", "resume.pdf", quota.ErrExceeded)
	require.Equal(t, http.StatusPaymentRequired, w.Code)
	require.Empty(t, w.Header().Get("X-Jobifai-Document-Version"))
	require.Contains(t, w.Body.String(), "insufficient credits")
}
