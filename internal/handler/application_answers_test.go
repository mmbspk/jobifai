package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestApplicationAnswers_HTTPConditionalSaveAndPartialPatch(t *testing.T) {
	svc, database := newTestServices(t)
	answers := applicationanswers.NewService(database, svc.Config.(*config.Store))
	answers.ConfirmMapping = bot.ConfirmFormMappingTx
	svc.ApplicationAnswers = answers
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "answers-http@fixture.test", "password123")
	original := domain.ResumeProfile{Summary: "Original", ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "Two weeks", FormAnswers: map[string]string{"mapping_legacy": "preserved"}}}
	require.Equal(t, 200, authPost(t, router, "/api/settings/resume", token, original).Code)
	response := authGet(t, router, "/api/settings/resume", token)
	require.Equal(t, 200, response.Code)
	require.NotEmpty(t, response.Header().Get("ETag"))
	var profileResponse struct {
		domain.ResumeProfile
		Revision string `json:"profile_revision"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profileResponse))
	require.NotEmpty(t, profileResponse.Revision)
	response = authGet(t, router, "/api/settings/application-answers", token)
	require.Equal(t, 200, response.Code)
	var facts domain.ApplicationAnswersView
	require.NoError(t, json.NewDecoder(response.Body).Decode(&facts))
	require.Len(t, facts.Catalog, 10)
	raw, _ := json.Marshal(map[string]any{"revision": facts.Revision, "topics": map[string]any{"notice_period": map[string]string{"notice_period": "Immediately"}}})
	request := httptest.NewRequest(http.MethodPatch, "/api/settings/application-answers", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	request = httptest.NewRequest(http.MethodPost, "/api/settings/resume", bytes.NewReader(mustJSON(t, original)))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("If-Match", `"`+profileResponse.Revision+`"`)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 409, recorder.Code, recorder.Body.String())
	response = authGet(t, router, "/api/settings/resume", token)
	var saved domain.ResumeProfile
	require.NoError(t, json.NewDecoder(response.Body).Decode(&saved))
	require.Equal(t, "Immediately", saved.ApplicationDefaults.NoticePeriod)
	require.Equal(t, "preserved", saved.ApplicationDefaults.FormAnswers["mapping_legacy"])
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestApplicationAnswers_HTTPInboxOwnershipReadAndOlderMapping(t *testing.T) {
	svc, database := newTestServices(t)
	answers := applicationanswers.NewService(database, svc.Config.(*config.Store))
	answers.ConfirmMapping = bot.ConfirmFormMappingTx
	svc.ApplicationAnswers = answers
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "review-http@fixture.test", "password123")
	other := registerAndLogin(t, router, "review-http-other@fixture.test", "password123")
	var userID string
	require.NoError(t, database.QueryRow(`SELECT id FROM users WHERE email=?`, "review-http@fixture.test").Scan(&userID))
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "Two weeks"}}
	require.NoError(t, svc.Config.Set(userID, "resume_profile", profile))
	store := &bot.DBMappingStore{DB: database, UserID: userID}
	var mappingID string
	for n := 0; n < 105; n++ {
		snap := bot.StepSnapshot{Platform: "seek", EmployerKey: "fixture", Fields: []bot.SnapField{{Ref: "f0", Label: fmt.Sprintf("Notice question %d", n), Type: bot.SnapTypeText}}}
		require.NoError(t, store.RecordProposals(context.Background(), snap, bot.StepInterpretation{Fields: []bot.FieldResolution{{Ref: "f0", Source: bot.SourceReview, Action: bot.ActionSkip, Pending: true}}}))
		if n == 0 {
			require.NoError(t, database.QueryRow(`SELECT id FROM form_field_mappings WHERE user_id=?`, userID).Scan(&mappingID))
		}
	}
	id, err := answers.RecordEncounter(context.Background(), userID, domain.ApplicationAnswerEncounter{ID: "http-encounter", FieldSignature: "field", MappingID: mappingID, Question: "What is your notice period?", Status: "required", ProfileRevision: applicationanswers.ProfileRevision(&profile), Platform: "seek", Employer: "fixture"})
	require.NoError(t, err)
	response := authGet(t, router, "/api/settings/application-answers/reviews", token)
	require.Equal(t, 200, response.Code)
	var page domain.ApplicationAnswerReviewPage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	require.Equal(t, 1, page.UnresolvedCount)
	require.Equal(t, 1, page.UnreadCount)
	response = authPost(t, router, "/api/settings/application-answers/reviews/read", token, map[string]any{})
	require.Equal(t, 204, response.Code)
	response = authGet(t, router, "/api/settings/application-answers/reviews", token)
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	require.Equal(t, 1, page.UnresolvedCount)
	require.Zero(t, page.UnreadCount)
	path := "/api/settings/application-answers/reviews/" + id + "/confirm"
	require.Equal(t, 404, authPost(t, router, path, other, map[string]any{"answer": "Two weeks", "revision": 1}).Code)
	response = authPost(t, router, path, token, map[string]any{"answer": "Two weeks", "revision": 1})
	require.Equal(t, 200, response.Code, response.Body.String())
	var state string
	require.NoError(t, database.QueryRow(`SELECT provenance FROM form_field_mappings WHERE id=?`, mappingID).Scan(&state))
	require.Equal(t, "confirmed_by_reviewer", state)
	require.Equal(t, 409, authPost(t, router, path, token, map[string]any{"answer": "Two weeks", "revision": 1}).Code)
	// Legacy direct-ID compatibility also works beyond its list's latest 100.
	response = authPost(t, router, "/api/settings/form-mappings/"+mappingID+"/confirm", token, map[string]any{"confirmed": true, "binding_key": "application_defaults.notice_period"})
	require.Equal(t, 200, response.Code, response.Body.String())
}
