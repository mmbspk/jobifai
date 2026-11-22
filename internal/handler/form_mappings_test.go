package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestFormMappings_ExplicitReviewAndUserIsolation(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "mapping-owner@example.com", "password123")
	other := registerAndLogin(t, router, "mapping-other@example.com", "password123")
	var userID string
	require.NoError(t, db.QueryRow("SELECT id FROM users WHERE email=?", "mapping-owner@example.com").Scan(&userID))
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "2 weeks"}}
	require.NoError(t, svc.Config.Set(userID, "resume_profile", profile))
	snap := bot.StepSnapshot{V: bot.SnapshotSchemaV1, Platform: "seek", EmployerKey: "acme", Fields: []bot.SnapField{{Ref: "f0", Label: "How soon can you leave?", Type: bot.SnapTypeText}}}
	store := &bot.DBMappingStore{DB: db, UserID: userID}
	require.NoError(t, store.RecordProposals(context.Background(), snap, bot.StepInterpretation{Fields: []bot.FieldResolution{{Ref: "f0", Source: bot.SourceLLM, Action: bot.ActionFill, IntentType: bot.FieldIntentProfile, BindingKey: "application_defaults.notice_period"}}}))
	response := authGet(t, router, "/api/settings/form-mappings", token)
	require.Equal(t, 200, response.Code)
	var list struct {
		Entries []bot.FormMappingReview `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&list))
	require.Len(t, list.Entries, 1)
	id := list.Entries[0].ID
	path := "/api/settings/form-mappings/" + id + "/confirm"
	assert.Equal(t, 400, authPost(t, router, path, token, map[string]any{"binding_key": "application_defaults.notice_period"}).Code)
	assert.Equal(t, 404, authPost(t, router, path, other, map[string]any{"confirmed": true, "binding_key": "application_defaults.notice_period"}).Code)
	response = authPost(t, router, path, token, map[string]any{"confirmed": true, "binding_key": "application_defaults.notice_period"})
	require.Equal(t, 200, response.Code, response.Body.String())
	_, hit := store.LookupField(snap.Fields[0].FieldSignature("seek"), "acme")
	assert.True(t, hit)
	assert.Equal(t, 404, authDelete(t, router, "/api/settings/form-mappings/"+id, other).Code)
	assert.Equal(t, 200, authDelete(t, router, "/api/settings/form-mappings/"+id, token).Code)
	_, hit = store.LookupField(snap.Fields[0].FieldSignature("seek"), "acme")
	assert.False(t, hit)
}

func TestFormMappings_ReviewedAnswerStaysInCandidateProfile(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "mapping-answer@example.com", "password123")
	var userID string
	require.NoError(t, db.QueryRow("SELECT id FROM users WHERE email=?", "mapping-answer@example.com").Scan(&userID))
	require.NoError(t, svc.Config.Set(userID, "resume_profile", domain.ResumeProfile{}))
	snap := bot.StepSnapshot{Platform: "linkedin", Fields: []bot.SnapField{{Ref: "f0", Label: "Do you have the required licence?", Type: bot.SnapTypeRadio, Options: []string{"Yes", "No"}}}}
	store := &bot.DBMappingStore{DB: db, UserID: userID}
	require.NoError(t, store.RecordProposals(context.Background(), snap, bot.StepInterpretation{Fields: []bot.FieldResolution{{Ref: "f0", Source: bot.SourceReview, Pending: true}}}))
	var id string
	require.NoError(t, db.QueryRow("SELECT id FROM form_field_mappings WHERE user_id=?", userID).Scan(&id))
	response := authPost(t, router, "/api/settings/form-mappings/"+id+"/confirm", token, map[string]any{"confirmed": true, "answer": "Yes"})
	require.Equal(t, 200, response.Code, response.Body.String())
	var profile domain.ResumeProfile
	require.NoError(t, svc.Config.Get(userID, "resume_profile", &profile))
	assert.Equal(t, "Yes", profile.ApplicationDefaults.FormAnswers["mapping_"+id])
	var savedValue string
	require.NoError(t, db.QueryRow("SELECT resolved_value FROM form_field_mappings WHERE id=?", id).Scan(&savedValue))
	assert.Empty(t, savedValue)
}

func TestFormMappings_FailedReviewRollsBackCandidateAnswer(t *testing.T) {
	for _, cause := range []string{"invalid option", "promotion failure"} {
		t.Run(cause, func(t *testing.T) {
			svc, db := newTestServices(t)
			router := handler.NewRouter(svc)
			token := registerAndLogin(t, router, "mapping-rollback@example.com", "password123")
			var userID string
			require.NoError(t, db.QueryRow("SELECT id FROM users WHERE email=?", "mapping-rollback@example.com").Scan(&userID))
			original := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "3 weeks", FormAnswers: map[string]string{"existing": "preserved"}}}
			require.NoError(t, svc.Config.Set(userID, "resume_profile", original))
			snap := bot.StepSnapshot{Platform: "seek", Fields: []bot.SnapField{{Ref: "f0", Label: "Required licence?", Type: bot.SnapTypeRadio, Options: []string{"Yes", "No"}}}}
			store := &bot.DBMappingStore{DB: db, UserID: userID}
			require.NoError(t, store.RecordProposals(context.Background(), snap, bot.StepInterpretation{Fields: []bot.FieldResolution{{Ref: "f0", Source: bot.SourceReview, Pending: true}}}))
			var id string
			require.NoError(t, db.QueryRow("SELECT id FROM form_field_mappings WHERE user_id=?", userID).Scan(&id))
			answer := "Maybe"
			if cause == "promotion failure" {
				answer = "Yes"
				_, err := db.Exec(`CREATE TRIGGER reject_mapping_promotion BEFORE UPDATE ON form_field_mappings WHEN NEW.provenance='confirmed_by_reviewer' BEGIN SELECT RAISE(ABORT,'fixture promotion rejected'); END`)
				require.NoError(t, err)
			}
			response := authPost(t, router, "/api/settings/form-mappings/"+id+"/confirm", token, map[string]any{"confirmed": true, "answer": answer})
			assert.Equal(t, 400, response.Code, response.Body.String())
			var current domain.ResumeProfile
			require.NoError(t, svc.Config.Get(userID, "resume_profile", &current))
			assert.Equal(t, original, current)
			var provenance string
			require.NoError(t, db.QueryRow("SELECT provenance FROM form_field_mappings WHERE id=?", id).Scan(&provenance))
			assert.Equal(t, "pending", provenance)
		})
	}
}
