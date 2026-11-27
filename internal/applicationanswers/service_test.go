package applicationanswers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func setup(t *testing.T) (*applicationanswers.Service, *config.Store, *sql.DB) {
	t.Helper()
	database, err := appdb.Open(filepath.Join(t.TempDir(), "answers.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec(`INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture'),('other','other@fixture.test','fixture')`)
	require.NoError(t, err)
	cfg := config.NewStore(database)
	return applicationanswers.NewService(database, cfg), cfg, database
}

func patch(t *testing.T, id string, value any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return map[string]json.RawMessage{id: raw}
}

func TestApplicationAnswerPatchPreservesLegacyFactsAndConcurrentCV(t *testing.T) {
	svc, cfg, _ := setup(t)
	no := false
	amount := 150000.0
	initial := domain.ResumeProfile{Summary: "Original CV", ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: &no, WorkRights: []domain.ApplicationWorkRight{{Country: "Australia", Status: "Permanent resident", RequiresSponsorship: &no}, {Country: "Germany", Status: "Unknown"}}, SalaryRange: &domain.ApplicationSalaryRange{Minimum: &amount, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &no}, CustomAnswers: []domain.ApplicationAnswer{{Question: "Travel?", Answer: "Occasional", Country: "Australia"}}, FormAnswers: map[string]string{"mapping_legacy": "Saved answer"}}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", initial))
	view, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	require.Len(t, view.Catalog, 10)
	core := 0
	for _, topic := range view.Catalog {
		if topic.Core {
			core++
		}
	}
	require.Equal(t, 5, core)
	initial.Summary = "Newer unsaved-to-this-screen CV"
	require.NoError(t, cfg.Set("candidate", "resume_profile", initial))
	out, err := svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Immediately", "availability": "Weekdays"})})
	require.NoError(t, err)
	require.Equal(t, "Newer unsaved-to-this-screen CV", out.Profile.Summary)
	require.Equal(t, "Saved answer", out.ApplicationDefaults.FormAnswers["mapping_legacy"])
	require.Equal(t, initial.ApplicationDefaults.WorkRights, out.ApplicationDefaults.WorkRights)
	require.Equal(t, initial.ApplicationDefaults.CustomAnswers, out.ApplicationDefaults.CustomAnswers)
	require.Equal(t, initial.ApplicationDefaults.SalaryRange, out.ApplicationDefaults.SalaryRange)
	require.NotNil(t, out.ApplicationDefaults.RequiresSponsorship)
	require.False(t, *out.ApplicationDefaults.RequiresSponsorship)
	_, err = svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "travel", "Frequent")})
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
}

func TestApplicationAnswersConcurrentPatchesCannotLoseFacts(t *testing.T) {
	svc, _, _ := setup(t)
	view, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	errs := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, value := range []string{"Two weeks", "Four weeks"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			<-start
			_, err := svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "notice_period", map[string]string{"notice_period": value})})
			errs <- err
		}(value)
	}
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, applicationanswers.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
}

func TestApplicationAnswerSourcesDoNotChangeProfileOrWarmRevision(t *testing.T) {
	svc, cfg, _ := setup(t)
	p := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "Two weeks"}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	before, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	require.NoError(t, svc.RecordSources(context.Background(), "candidate", map[string]domain.ApplicationAnswerSource{"notice_period": {Source: "cv", Evidence: []string{"Notice period: two weeks"}}}, applicationanswers.ProfileRevision(&p)))
	after, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	require.Equal(t, applicationanswers.ProfileRevision(before.Profile), applicationanswers.ProfileRevision(after.Profile))
	require.Equal(t, "cv", after.Sources["notice_period"].Source)
	p.ApplicationDefaults.NoticePeriod = "One month"
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	after, err = svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	require.Equal(t, "profile", after.Sources["notice_period"].Source)
}

func encounter(p *domain.ResumeProfile, id, signature, country string) domain.ApplicationAnswerEncounter {
	return domain.ApplicationAnswerEncounter{ID: id, FieldSignature: signature, Question: "Describe your availability", Status: "suggested", ProposedAnswer: "Available on weekdays", Country: country, Employer: "Fixture employer", Platform: "fixture", ProfileRevision: applicationanswers.ProfileRevision(p), Evidence: []string{"Weekdays"}, Reason: "Supported phrasing", Source: "llm"}
}

func TestApplicationAnswerReviewGroupingReadAndImmutableActualUse(t *testing.T) {
	svc, cfg, database := setup(t)
	p := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{Availability: "Weekdays"}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	e := encounter(&p, "one", "same-field", "Australia")
	e.AttemptID = "attempt"
	id, err := svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	known, err := svc.KnownMissing(context.Background(), "candidate", e)
	require.NoError(t, err)
	require.False(t, known)
	_, err = svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	e.ID = "two"
	_, err = svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	e.ID = "three"
	e.Country = "Germany"
	_, err = svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	page, err := svc.ListReviews(context.Background(), "candidate", "", "", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, 2, page.UnresolvedCount)
	require.Equal(t, 1, page.UnreadCount)
	require.NoError(t, svc.MarkRead(context.Background(), "candidate"))
	page, err = svc.ListReviews(context.Background(), "candidate", "", "", 20)
	require.NoError(t, err)
	require.Equal(t, 2, page.UnresolvedCount)
	require.Zero(t, page.UnreadCount)
	e.ID = "repeat-after-read"
	e.Country = "Australia"
	_, err = svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	page, err = svc.ListReviews(context.Background(), "candidate", "", "", 20)
	require.NoError(t, err)
	require.Zero(t, page.UnreadCount, "unchanged grouped question must not create another unread notification")
	require.NoError(t, svc.MarkFilled(context.Background(), "candidate", "one", "I am available on weekdays."))
	require.NoError(t, svc.MarkFilled(context.Background(), "candidate", "one", "I am available on weekdays."))
	require.ErrorIs(t, svc.MarkFilled(context.Background(), "candidate", "one", "Different answer"), applicationanswers.ErrConflict)
	_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,outcome) VALUES('attempt','candidate','fixture','submitted_confirmed')`)
	require.NoError(t, err)
	detail, err := svc.GetReview(context.Background(), "candidate", id)
	require.NoError(t, err)
	require.Len(t, detail.Encounters, 3)
	found := false
	for _, saved := range detail.Encounters {
		if saved.ID == "one" {
			found = true
			require.Equal(t, "I am available on weekdays.", saved.AnswerUsed)
			require.Equal(t, "submitted", saved.UseStatus)
		}
	}
	require.True(t, found)
	_, err = database.Exec(`UPDATE apply_attempts SET outcome='submission_unknown' WHERE id='attempt'`)
	require.NoError(t, err)
	detail, err = svc.GetReview(context.Background(), "candidate", id)
	require.NoError(t, err)
	for _, saved := range detail.Encounters {
		if saved.ID == "one" {
			require.Equal(t, "submission_unknown", saved.UseStatus)
		}
	}
	_, err = svc.GetReview(context.Background(), "other", id)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestApplicationAnswerCVOriginRequiresExplicitMatchingSave(t *testing.T) {
	svc, cfg, _ := setup(t)
	draft := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "Two weeks"}}
	prefill := []domain.CVAnswerPrefill{{TopicID: "notice_period", Binding: "application_defaults.notice_period", Source: "cv_evidence", ReviewState: "suggested", Evidence: []domain.CVAnswerEvidence{{Quote: "Notice period: two weeks", Start: 0, End: 24}}}, {TopicID: "travel", Source: "model_suggestion", NeedsConfirmation: true, Evidence: []domain.CVAnswerEvidence{{Quote: "Travelled for previous role"}}}}
	importID, err := svc.CaptureImport(context.Background(), "candidate", &draft, prefill)
	require.NoError(t, err)
	require.NotEmpty(t, importID)
	var saved domain.ResumeProfile
	err = cfg.Get("candidate", "resume_profile", &saved)
	require.ErrorIs(t, err, domain.ErrNotFound, "extracting evidence must not save candidate facts")
	before, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	other, err := svc.Get(context.Background(), "other")
	require.NoError(t, err)
	_, err = svc.Patch(context.Background(), "other", domain.ApplicationAnswersPatch{Revision: other.Revision, ImportID: importID, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Two weeks"})})
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
	err = cfg.Get("other", "resume_profile", &saved)
	require.ErrorIs(t, err, domain.ErrNotFound, "wrong-user origin token must roll back the fact write")
	out, err := svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: before.Revision, ImportID: importID, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Two weeks"})})
	require.NoError(t, err)
	require.Equal(t, "cv", out.Sources["notice_period"].Source)
	require.Equal(t, []string{"Notice period: two weeks"}, out.Sources["notice_period"].Evidence)
	out, err = svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: out.Revision, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Immediately"})})
	require.NoError(t, err)
	require.Equal(t, "confirmed", out.Sources["notice_period"].Source)
	require.Empty(t, out.Sources["notice_period"].Evidence)
	out, err = svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: out.Revision, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Two weeks"})})
	require.NoError(t, err)
	require.Equal(t, "confirmed", out.Sources["notice_period"].Source, "manual entry cannot borrow an unreferenced old CV origin")
	out, err = svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: out.Revision, Topics: patch(t, "travel", "Occasional")})
	require.NoError(t, err)
	require.Equal(t, "confirmed", out.Sources["travel"].Source)
}

func TestApplicationAnswerReviewPaginationAndAtomicConfirm(t *testing.T) {
	svc, cfg, _ := setup(t)
	p := domain.ResumeProfile{Summary: "Saved profile"}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	var oldest string
	for n := 0; n < 105; n++ {
		e := encounter(&p, fmt.Sprint(n), fmt.Sprint(n), "Australia")
		e.MappingID = fmt.Sprint("mapping-", n)
		id, err := svc.RecordEncounter(context.Background(), "candidate", e)
		require.NoError(t, err)
		if n == 0 {
			oldest = id
		}
	}
	page, err := svc.ListReviews(context.Background(), "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 20)
	require.NotEmpty(t, page.NextCursor)
	seen := map[string]bool{}
	for {
		for _, item := range page.Items {
			require.False(t, seen[item.ID])
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		page, err = svc.ListReviews(context.Background(), "candidate", page.NextCursor, "active", 20)
		require.NoError(t, err)
	}
	require.Len(t, seen, 105)
	detail, err := svc.GetReview(context.Background(), "candidate", oldest)
	require.NoError(t, err)
	svc.ConfirmMapping = func(context.Context, *sql.Tx, string, string, string, *domain.ResumeProfile) error {
		return fmt.Errorf("incompatible choices")
	}
	_, err = svc.Confirm(context.Background(), "candidate", oldest, domain.ApplicationAnswerConfirmation{Answer: "Reviewed answer", Revision: detail.Revision})
	require.Error(t, err)
	var unchanged domain.ResumeProfile
	require.NoError(t, cfg.Get("candidate", "resume_profile", &unchanged))
	require.Nil(t, unchanged.ApplicationDefaults.FormAnswers)
	after, err := svc.GetReview(context.Background(), "candidate", oldest)
	require.NoError(t, err)
	require.Equal(t, detail.Revision, after.Revision)
	require.Equal(t, "active", after.Lifecycle)
	var gotMapping string
	svc.ConfirmMapping = func(_ context.Context, _ *sql.Tx, userID, id, binding string, p *domain.ResumeProfile) error {
		require.Equal(t, "candidate", userID)
		gotMapping = id
		require.Equal(t, "Reviewed answer", p.ApplicationDefaults.FormAnswers["mapping_"+id])
		return nil
	}
	_, err = svc.Confirm(context.Background(), "candidate", oldest, domain.ApplicationAnswerConfirmation{Answer: "Reviewed answer", Revision: detail.Revision})
	require.NoError(t, err)
	require.Equal(t, "mapping-0", gotMapping)
	_, err = svc.Confirm(context.Background(), "candidate", oldest, domain.ApplicationAnswerConfirmation{Answer: "Reviewed answer", Revision: detail.Revision})
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
	after, err = svc.GetReview(context.Background(), "candidate", oldest)
	require.NoError(t, err)
	require.Equal(t, "resolved", after.Lifecycle)
	require.Equal(t, "Available on weekdays", after.Encounters[0].ProposedAnswer)
	require.Empty(t, after.Encounters[0].AnswerUsed)
}

func TestApplicationAnswerStaleProfileAndCrossUserActions(t *testing.T) {
	svc, cfg, _ := setup(t)
	p := domain.ResumeProfile{}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	id, err := svc.RecordEncounter(context.Background(), "candidate", encounter(&p, "record", "field", "Australia"))
	require.NoError(t, err)
	_, err = svc.Confirm(context.Background(), "other", id, domain.ApplicationAnswerConfirmation{Answer: "No", Revision: 1})
	require.ErrorIs(t, err, domain.ErrNotFound)
	p.Summary = "Changed"
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	_, err = svc.Confirm(context.Background(), "candidate", id, domain.ApplicationAnswerConfirmation{Answer: "No", Revision: 1})
	require.ErrorIs(t, err, applicationanswers.ErrConflict)
}

func TestApplicationAnswerCommonCorrectionKeepsOtherMissingFactsActionable(t *testing.T) {
	svc, cfg, _ := setup(t)
	p := domain.ResumeProfile{}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	e := encounter(&p, "missing-work-rights", "rights", "Australia")
	e.Status = "required"
	e.Reason = domain.ApplicationAnswerMissingFactReason
	e.ProposedAnswer = ""
	e.TopicID = "work_rights"
	id, err := svc.RecordEncounter(context.Background(), "candidate", e)
	require.NoError(t, err)
	known, err := svc.KnownMissing(context.Background(), "candidate", e)
	require.NoError(t, err)
	require.True(t, known)
	view, err := svc.Get(context.Background(), "candidate")
	require.NoError(t, err)
	out, err := svc.Patch(context.Background(), "candidate", domain.ApplicationAnswersPatch{Revision: view.Revision, Topics: patch(t, "notice_period", map[string]string{"notice_period": "Immediately"})})
	require.NoError(t, err)
	e.ProfileRevision = out.ProfileRevision
	known, err = svc.KnownMissing(context.Background(), "candidate", e)
	require.NoError(t, err)
	require.False(t, known, "rebasing review state must not make old facts a new no-model-call proof")
	detail, err := svc.GetReview(context.Background(), "candidate", id)
	require.NoError(t, err)
	require.Equal(t, "active", detail.Lifecycle)
	page, err := svc.ListReviews(context.Background(), "candidate", "", "", 20)
	require.NoError(t, err)
	require.Equal(t, 1, page.UnresolvedCount)
}
