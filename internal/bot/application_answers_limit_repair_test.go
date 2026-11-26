package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

// The browser changes the declared cap after a verified fill. A fresh grounded
// answer must replace the old use evidence before this fixture can submit.
func TestApplicationAnswerChangedLimitRepairsWithFreshVerifiedHistory(t *testing.T) {
	ctx := context.Background()
	browser := launchTestBrowser(t)
	page := browser.MustPage("")
	require.NoError(t, page.SetDocumentContent(`<html><body><div role="dialog" class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="experience">Describe your relevant experience</label>
 <textarea id="experience" required maxlength="100"></textarea></div>
 <button id="submit" onclick="window.submitted=document.getElementById('experience').value">Submit application</button>
 </div></body></html>`))
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	profile := groundedFixtureProfile()
	cfg := config.NewStore(database)
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	svc := applicationanswers.NewService(database, cfg)
	store := &DBMappingStore{DB: database, UserID: "candidate"}
	const original = "I coordinate client projects and improve scheduling processes."
	const repaired = "I coordinate client projects."
	server := newGroundedCountingServer(t, func(body []byte) string {
		value := original
		if strings.Contains(string(body), `\"max_chars\":30`) {
			value = repaired
		}
		refs := extractRefsFromPromptBody(body)
		proposals := make([]LLMFieldProposal, 0, len(refs))
		for _, ref := range refs {
			proposals = append(proposals, LLMFieldProposal{Ref: ref, Action: "fill", IntentType: FieldIntentLLM, Value: value, Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}}})
		}
		raw, _ := json.Marshal(proposals)
		return string(raw)
	})
	b := New(Config{DB: database, UserID: "candidate", Profile: profile, ApplicationAnswers: svc, AdaptiveMapping: store, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t)), Platform: domain.PlatformLinkedIn})
	lazy := &lazyDocGen{b: b, attemptID: newAttemptID(), job: linkedInJob{ID: "limit-fixture", Company: "Fixture", Title: "Coordinator"}}
	ac := newAttemptContext("candidate", "linkedin", lazy.job.ID, "https://fixture.invalid/job", lazy.job.Title, lazy.job.Company, "fixture", "")
	ac.AttemptID = lazy.attemptID
	beginAttempt(database, ac)
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	require.NoError(t, b.prepareLegacyAnswers(ctx, lazy, fields))
	r := lazy.answerPlan[legacyFieldKey(fields[0])]
	require.Equal(t, original, r.Value)
	result := fillAdaptiveControl(ctx, page, lazy.answerSnapshot.Fields[0], r.Value, profile)
	require.NoError(t, result.Err)
	require.True(t, result.Filled)
	require.NoError(t, b.markLegacyAnswerFilled(ctx, lazy, fields[0], r.Value))
	require.Equal(t, int32(2), server.requests.Load())
	_, err = page.Eval(`() => { document.getElementById('experience').maxLength=30; }`)
	require.NoError(t, err)
	violations, err := b.linkedInTextLimitViolations(page, true)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	require.Equal(t, 30, violations[0].MaxChars)
	fixed, err := b.refillLinkedInOverLimitFields(ctx, page, lazy)
	require.NoError(t, err)
	require.True(t, fixed)
	require.Equal(t, int32(4), server.requests.Load(), "changed constraint needs a fresh proposal and independent verification")
	_, err = page.Eval(`() => document.getElementById('submit').click()`)
	require.NoError(t, err)
	actual, err := page.Eval(`() => window.submitted`)
	require.NoError(t, err)
	require.Equal(t, repaired, actual.Value.String(), "submit receives the intact newly verified answer")
	finishAttempt(database, ac, OutcomeSubmittedConfirmed, "apply", "fixture", "")
	history, err := svc.ListReviews(ctx, "candidate", "", "history", 20)
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	require.Equal(t, original, history.Items[0].AnswerUsed)
	require.Equal(t, "replaced", history.Items[0].UseStatus)
	active, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, active.Items, 1)
	require.Equal(t, repaired, active.Items[0].AnswerUsed)
	require.Equal(t, "submitted", active.Items[0].UseStatus)
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(ctx, lazy.answerSnapshot, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.False(t, warm.LLMUsed)
	require.Equal(t, repaired, warm.Fields[0].Value)
	require.Equal(t, int32(4), server.requests.Load())
}
