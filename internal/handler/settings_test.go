package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestSettings_Styles_FriendlyLabelsPreserveValuesAndHideSharedLayouts(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.StylesDir = t.TempDir()
	for _, name := range []string{"style_au.css", "style_us.css", "style_my_design.css", "_base_a4.css", "_base_letter.css"} {
		require.NoError(t, os.WriteFile(filepath.Join(svc.StylesDir, name), []byte("body {}"), 0o600))
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "friendly-styles@example.com", "password123")
	w := authGet(t, router, "/api/settings/styles", token)
	require.Equal(t, http.StatusOK, w.Code)
	var styles []domain.ResumeStyle
	require.NoError(t, json.NewDecoder(w.Body).Decode(&styles))
	require.Len(t, styles, 3)
	labels := map[string]string{}
	for _, style := range styles {
		labels[style.Name] = style.DisplayName
	}
	assert.Equal(t, map[string]string{"Au": "Australia", "Us": "United States", "My Design": "My Design"}, labels)
}

// postResumeFile uploads bytes as a resume_file multipart field.
func postResumeFile(t *testing.T, router http.Handler, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("resume_file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/settings/resume/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestSettings_General_EmptyReturns200(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "gen1@example.com", "password123")

	w := authGet(t, router, "/api/settings/general", token)
	assert.Equal(t, 200, w.Code)
}

func TestSettings_General_RoundTrip(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "gen2@example.com", "password123")

	gs := domain.GeneralSettings{JobSuitabilityScore: 7, HalalJobFilter: true}
	wPost := authPost(t, router, "/api/settings/general", token, gs)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/settings/general", token)
	assert.Equal(t, 200, wGet.Code)

	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, 7, got.JobSuitabilityScore)
	assert.True(t, got.HalalJobFilter)
}

func TestSettings_General_UserDailyApplicationLimit(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "gen-daily@example.com", "password123")

	gs := domain.GeneralSettings{
		JobSuitabilityScore: 7,
		HumanBehavior:       domain.HumanBehaviorConfig{DailyApplicationLimit: 15},
	}
	wPost := authPost(t, router, "/api/settings/general", token, gs)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/settings/general", token)
	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, 15, got.HumanBehavior.DailyApplicationLimit)
}

func TestSettings_Preferences_EmptyReturns200(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "pref1@example.com", "password123")

	w := authGet(t, router, "/api/settings/preferences", token)
	assert.Equal(t, 200, w.Code)
}

func TestSettings_Preferences_RoundTrip(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "pref2@example.com", "password123")

	prefs := domain.WorkPreferences{
		Positions: []string{"Data Analyst", "Business Analyst"},
		Remote:    true,
	}
	wPost := authPost(t, router, "/api/settings/preferences", token, prefs)
	assert.Equal(t, 200, wPost.Code)

	wGet := authGet(t, router, "/api/settings/preferences", token)
	assert.Equal(t, 200, wGet.Code)

	var got domain.WorkPreferences
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, []string{"Data Analyst", "Business Analyst"}, got.Positions)
	assert.True(t, got.Remote)
}

func TestSettings_Secrets_EmptyResponse(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "sec1@example.com", "password123")

	w := authGet(t, router, "/api/settings/secrets", token)
	assert.Equal(t, 200, w.Code)

	var out domain.SecretsConfig
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Empty(t, out.LLMAPIKey)
	assert.Empty(t, out.CredentialPlatforms)
}

func TestSettings_Secrets_APIKeyMasked(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "sec2@example.com"
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)

	wSet := authPost(t, router, "/api/admin/system/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key", "value": "sk-real-secret",
	})
	assert.Equal(t, 200, wSet.Code)

	wGet := authGet(t, router, "/api/admin/system/secrets", token)
	var out map[string]any
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&out))
	assert.Equal(t, true, out["has_default_api_key"])
	assert.Equal(t, "****", out["llm_api_key"])
}

func TestAdmin_SystemAPIKeyForbiddenForNonAdmin(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "sec3@example.com", "password123")

	wSet := authPost(t, router, "/api/admin/system/secrets/api-key", token, map[string]string{
		"key_type": "llm_api_key", "value": "sk-real-secret",
	})
	assert.Equal(t, 403, wSet.Code)
}

func TestSettings_General_NonAdminCannotOverwriteLLM(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "gen3@example.com"
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)

	adminGS := domain.GeneralSettings{
		LLM: domain.LLMConfig{Provider: "claude", Model: "claude-admin-model"},
	}
	wAdmin := authPost(t, router, "/api/settings/general", token, adminGS)
	assert.Equal(t, 200, wAdmin.Code)

	_, err := db.Exec(`UPDATE users SET is_admin = 0 WHERE email = ?`, email)
	require.NoError(t, err)

	userGS := domain.GeneralSettings{
		JobSuitabilityScore: 8,
		LLM:                 domain.LLMConfig{Provider: "openai", Model: "gpt-hacked"},
	}
	wUser := authPost(t, router, "/api/settings/general", token, userGS)
	assert.Equal(t, 200, wUser.Code)

	wGet := authGet(t, router, "/api/settings/general", token)
	var got domain.GeneralSettings
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, 8, got.JobSuitabilityScore)
	assert.Empty(t, got.LLM.Model)

	setUserAdmin(t, db, email)
	wGetAdmin := authGet(t, router, "/api/settings/general", token)
	require.NoError(t, json.NewDecoder(wGetAdmin.Body).Decode(&got))
	assert.Equal(t, "claude-admin-model", got.LLM.Model)
}

// TestSettings_ResumeUpload_YAML_NoLLMConfigured verifies that a valid YAML resume profile
// can be imported even when no LLM is configured for the user.
func TestSettings_ResumeUpload_YAML_NoLLMConfigured(t *testing.T) {
	svc, _ := newTestServices(t)
	// No LLMFactory wired: YAML upload must succeed without hitting an extractor.
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "yaml-nollm@example.com", "password123")

	yaml := []byte("personal_information:\n  name: Fatima Malik\n  email: fatima@example.com\nsummary: Experienced accountant\n")
	w := postResumeFile(t, router, token, "resume_profile.yaml", yaml)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var p domain.ResumeProfile
	require.NoError(t, json.NewDecoder(w.Body).Decode(&p))
	assert.Equal(t, "Fatima Malik", p.PersonalInformation.Name)
}

// TestSettings_ResumeUpload_NonYAML_NoLLMConfigured_Returns422 verifies that uploading a
// non-YAML file (e.g. PDF) when no LLM is configured still returns 422.
func TestSettings_ResumeUpload_NonYAML_NoLLMConfigured_Returns422(t *testing.T) {
	svc, _ := newTestServices(t)
	// LLMFactory is set but returns nil (no API key) — this is production behaviour when unconfigured.
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) { return nil, nil }
	svc.FileToText = noopFileToText
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "pdf-nollm@example.com", "password123")

	w := postResumeFile(t, router, token, "resume.pdf", []byte("%PDF-1.4 fake content"))
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
