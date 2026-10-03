package documents_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestResolveReviewPreflight_ReuseZeroCredits(t *testing.T) {
	t.Parallel()
	in := documents.ResolveReviewInput{
		Policies: domain.DocumentPolicies{
			ResumeMode: domain.ResumeDocumentModeDefault,
			CoverMode:  domain.CoverDocumentModeSkipOptional,
			OnboardingComplete: true,
		},
		Defaults: documents.DefaultsView{ResumeVersionID: "rv1"},
		EffectiveResumeVersionID: "rv1",
		Selection: documents.ApplicationDocumentSelection{CoverSkip: true},
		Caps: documents.FormDocumentCapabilities{
			Detected: true, ResumeFileSlots: 1, CoverOptional: true,
		},
		HasConfirmedProfile: true,
		DefaultResumeExists: true,
	}
	pf, res := documents.ResolveReviewPreflight(in)
	require.False(t, res.Resume.NeedTailor)
	require.Equal(t, "reuse", pf.Resume.Action)
	require.NotNil(t, pf.Resume.CreditsEstimate)
	require.Equal(t, int64(0), *pf.Resume.CreditsEstimate)
	require.Equal(t, "skip", pf.Cover.Action)
}

func TestValidateUserVersionKind_WrongKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := documents.NewStore(sqlDB)
	svc := &documents.Service{Store: store}
	userID := "u1"
	docID, err := store.CreateDocument(ctx, userID, documents.KindCoverLetter, "cover")
	require.NoError(t, err)
	vID, _, err := store.InsertVersion(ctx, documents.InsertVersionParams{
		DocumentID: docID, UserID: userID,
		ContentKind: "cover_text", ContentJSON: `{"body":"hi"}`,
		Source: documents.SourceUserEdit,
	})
	require.NoError(t, err)
	err = svc.ValidateUserVersionKind(ctx, userID, vID, documents.KindResume)
	require.ErrorIs(t, err, documents.ErrReviewVersionWrongKind)
}

func TestPackAfterSelectionChange_ClearsPrepared(t *testing.T) {
	t.Parallel()
	pack := documents.PackAfterSelectionChange(documents.ApplicationDocumentSelection{ResumeVersionID: "a"})
	require.Contains(t, pack.HoldReason, "prepare again")
	require.False(t, pack.Prepared)
}

func TestResolveReviewPreflight_AIEstimateUsesEstimator(t *testing.T) {
	t.Parallel()
	in := documents.ResolveReviewInput{
		Policies: domain.DocumentPolicies{
			ResumeMode: domain.ResumeDocumentModeTailorJob,
			CoverMode:  domain.CoverDocumentModeWhenRequired,
			OnboardingComplete: true,
		},
		Caps: documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverRequired: true},
		HasConfirmedProfile: true,
		DefaultResumeExists: true,
		CreditEstimator: func(task string) (int64, bool) {
			if task == documents.AITaskTailorResume {
				return 123, true
			}
			return 0, false
		},
	}
	pf, _ := documents.ResolveReviewPreflight(in)
	require.Equal(t, "tailor", pf.Resume.Action)
	require.NotNil(t, pf.Resume.CreditsEstimate)
	require.Equal(t, int64(123), *pf.Resume.CreditsEstimate)
}

func TestResolver_FrozenIgnoresPolicyChange(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	res := r.Resolve(documents.ResolveInput{
		Policies: domain.DocumentPolicies{ResumeMode: domain.ResumeDocumentModeTailorJob, CoverMode: domain.CoverDocumentModeWhenRequired},
		Overrides: documents.ApplicationDocumentOverrides{Frozen: true, ResumeVersionID: "saved-v1", CoverVersionID: ""},
		Caps: documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverRequired: true},
		HasConfirmedProfile: true,
	})
	require.False(t, res.Resume.NeedTailor)
	require.Equal(t, "saved-v1", res.Resume.VersionID)
}

func TestResolver_ApplicationOverrideSiteAndSkip(t *testing.T) {
	t.Parallel()
	var r documents.Resolver
	res := r.Resolve(documents.ResolveInput{
		Policies: domain.DocumentPolicies{ResumeMode: domain.ResumeDocumentModeDefault, CoverMode: domain.CoverDocumentModeWhenRequired},
		Overrides: documents.ApplicationDocumentOverrides{ResumeUseSite: true, CoverSkip: true},
		Caps: documents.FormDocumentCapabilities{Detected: true, SiteResumePresent: true, CoverOptional: true},
		HasConfirmedProfile: true,
		DefaultResumeExists: true,
	})
	require.True(t, res.Resume.UseSite)
	require.True(t, res.Cover.Skip)
}
