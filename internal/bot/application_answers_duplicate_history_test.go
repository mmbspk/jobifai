package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/config"
)

func TestApplicationAnswerDuplicateIdlessControlsKeepBothSubmittedUses(t *testing.T) {
	ctx := context.Background()
	database := openMappingDB(t)
	_, err := database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	cfg := config.NewStore(database)
	profile := groundedFixtureProfile()
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	svc := applicationanswers.NewService(database, cfg)
	b := New(Config{DB: database, UserID: "candidate", Profile: profile, ApplicationAnswers: svc})
	lazy := &lazyDocGen{b: b, attemptID: newAttemptID(), job: linkedInJob{ID: "fixture", Company: "Fixture", Title: "Coordinator"}}
	ac := newAttemptContext("candidate", "linkedin", lazy.job.ID, "https://fixture.invalid/job", lazy.job.Title, lazy.job.Company, "fixture", "")
	ac.AttemptID = lazy.attemptID
	beginAttempt(database, ac)
	fields := []formField{{Index: 0, Type: "text", Question: "Describe your experience", Required: true, MaxChars: 100}, {Index: 1, Type: "text", Question: "Describe your experience", Required: true, MaxChars: 80}}
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "")
	server := newGroundedCountingServer(t, func(body []byte) string {
		return `[{"ref":"f0","action":"fill","intent_type":"llm","value":"I coordinate client projects.","evidence":[{"path":"/summary","quote":"I coordinate client projects and improve scheduling processes."}]},{"ref":"f1","action":"fill","intent_type":"llm","value":"I improve scheduling processes.","evidence":[{"path":"/summary","quote":"I coordinate client projects and improve scheduling processes."}]}]`
	})
	interp, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t))}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, interp.Paused)
	ids, err := b.prepareAnswerReviews(ctx, lazy, snap, interp, profile)
	require.NoError(t, err)
	require.Len(t, ids, 2)
	executor := NewStepExecutor(basicBounds(), noGuard())
	executor.AfterFieldFill = b.answerFillRecorder(ids)
	_, err = executor.ExecuteStep(ctx, snap, interp, &stubFiller{fillOK: true, submitReady: true})
	require.NoError(t, err)
	finishAttempt(database, ac, OutcomeSubmittedConfirmed, "apply", "fixture", "")
	page, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2, "separate id-less controls must not replace each other's history")
	for _, item := range page.Items {
		require.Equal(t, "submitted", item.UseStatus)
	}
	repaired := snap.Fields[1]
	repaired.Ref = "f0" // Single-field rescans renumber refs; browser ordinal stays stable.
	repaired.MaxChars = 30
	require.Equal(t, reviewEncounterIdentity(lazy, snap, snap.Fields[1], profile, false).FieldInstanceKey, reviewEncounterIdentity(lazy, snap, repaired, profile, false).FieldInstanceKey)
}
