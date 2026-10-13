package documents_test

// Real Documents→Resource→Quota integration test (Gap 6).
//
// Unlike the stub-based billing tests, these tests use real SQLite, real quota.Service,
// and real resource.Service wired as the documents.Service Biller. They assert that the
// resource_usage_events table is written and the quota balance decreases correctly.

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resource"
)

func newRealIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "integration_test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func enableRenderMetering(t *testing.T, cfg *config.Store) {
	t.Helper()
	require.NoError(t, cfg.Set(domain.SystemUserID, "resource_metering_defaults", domain.ResourceMeteringDefaults{
		Enabled: true,
		Render: domain.RenderMeteringConfig{
			Enabled:                  true,
			RawCostUSDMicroPerRender: 2_000, // $0.002 × 1.5 markup × 1000 c/$ = 3 credits
		},
	}))
}

func newRealIntegrationService(t *testing.T) (*documents.Service, *quota.Service, *resource.Service, *sql.DB, string) {
	t.Helper()
	db := newRealIntegrationDB(t)
	cfg := config.NewStore(db)

	// Set quota defaults.
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault:    true,
		CreditsPerUSD:         1000,
		ServiceMarkup:         0.5,
		PerCallFeeUSD:         0.002,
		TrialCredits:          500,
		TrialDays:             7,
		StarterCreditsMonthly: 3000,
		ProCreditsMonthly:     8000,
	}))
	enableRenderMetering(t, cfg)

	userID := "real-user-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Test User",
	)
	require.NoError(t, err)

	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))

	resourceSvc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)

	root := filepath.Join(t.TempDir(), "blobs")
	blobs, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)

	svc := &documents.Service{
		Store:     documents.NewStore(db),
		Blobs:     blobs,
		Renderer:  &countingRenderer{},
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		Biller:    resourceSvc,
		LoadProfile: func(uid string) (*domain.ResumeProfile, error) {
			if uid != userID {
				return nil, nil
			}
			return &domain.ResumeProfile{
				PersonalInformation: domain.PersonalInformation{
					Name:  "Integration Tester",
					Email: "tester@example.com",
				},
			}, nil
		},
	}
	return svc, quotaSvc, resourceSvc, db, userID
}

// TestRealIntegration_DocumentRender_UsageEventAndQuotaDecrease is the Gap 6 integration test.
// It creates a resume using a real DB, real quota.Service, and real resource.Service, then
// asserts that exactly one resource_usage_events row is written and the quota balance decreased.
func TestRealIntegration_DocumentRender_UsageEventAndQuotaDecrease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, quotaSvc, _, db, userID := newRealIntegrationService(t)

	rowBefore, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	require.Greater(t, rowBefore.TrialRemainingCredits, int64(0), "trial user must have positive balance")

	versionID, err := svc.CreateResumeFromProfile(ctx, userID, "Integration Resume", documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEmpty(t, versionID)

	// Assert exactly one resource_usage_events row for this render.
	expectedKey := "document-render:" + versionID
	var eventCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_usage_events WHERE user_id = ? AND idempotency_key = ?`,
		userID, expectedKey).Scan(&eventCount))
	assert.Equal(t, 1, eventCount, "exactly one usage event must be recorded")

	var resourceType string
	var creditsBurned int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT resource_type, credits_burned FROM resource_usage_events WHERE user_id = ? AND idempotency_key = ?`,
		userID, expectedKey).Scan(&resourceType, &creditsBurned))
	assert.Equal(t, string(domain.ResourceTypeDocumentRender), resourceType, "resource_type must be document_render")
	assert.Greater(t, creditsBurned, int64(0), "credits_burned must be positive")

	// Assert quota balance decreased by exactly credits_burned.
	rowAfter, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, rowBefore.TrialRemainingCredits-creditsBurned, rowAfter.TrialRemainingCredits,
		"trial balance must decrease by exactly the burned credits")
}

// TestRealIntegration_SecondRender_IndependentBurn verifies that a second render on the same
// document incurs a second independent billing event (no idempotency false-negative).
func TestRealIntegration_SecondRender_IndependentBurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, quotaSvc, _, db, userID := newRealIntegrationService(t)

	v1, err := svc.CreateResumeFromProfile(ctx, userID, "First", documents.RenderContext{Language: "en"})
	require.NoError(t, err)

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)

	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Test User", Email: "u@example.com"},
	}
	v2, err := svc.AppendResumeVersion(ctx, userID, list.Documents[0].ID, "Second", documents.SourceUserEdit, profile, documents.RenderContext{Language: "en"})
	require.NoError(t, err)
	require.NotEqual(t, v1, v2)

	var eventCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_usage_events WHERE user_id = ?`, userID).Scan(&eventCount))
	assert.Equal(t, 2, eventCount, "two renders must produce two independent billing events")

	var totalBurned int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(credits_burned),0) FROM resource_usage_events WHERE user_id = ?`, userID).Scan(&totalBurned))

	rowAfter, err := quotaSvc.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500)-totalBurned, rowAfter.TrialRemainingCredits, "balance must decrease by total burned")
}

// TestRealIntegration_QuotaExhausted_NoUsageEvent verifies that when BeforeRender returns
// ErrExceeded, no usage event is written and no version row is created.
func TestRealIntegration_QuotaExhausted_NoUsageEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db := newRealIntegrationDB(t)
	cfg := config.NewStore(db)

	// Give trial user only 1 credit so the 3-credit render fails quota check.
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
		PerCallFeeUSD:      0.002,
		TrialCredits:       1, // insufficient for a 3-credit render
		TrialDays:          7,
	}))
	enableRenderMetering(t, cfg)

	userID := "exhausted-user-" + t.Name()
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES (?, ?, 'x', ?, 0, CURRENT_TIMESTAMP)`,
		userID, userID+"@test.com", "Exhausted User",
	)
	require.NoError(t, err)

	quotaSvc := quota.NewService(db, cfg, nil)
	require.NoError(t, quotaSvc.InitTrial(ctx, userID))

	resourceSvc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)

	root := filepath.Join(t.TempDir(), "blobs")
	blobs, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)

	svc := &documents.Service{
		Store:     documents.NewStore(db),
		Blobs:     blobs,
		Renderer:  &countingRenderer{},
		MarketDir: filepath.Join("..", "..", "resume_markets"),
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		Biller:    resourceSvc,
		LoadProfile: func(uid string) (*domain.ResumeProfile, error) {
			return &domain.ResumeProfile{
				PersonalInformation: domain.PersonalInformation{Name: "Tester", Email: "t@test.com"},
			}, nil
		},
	}

	_, err = svc.CreateResumeFromProfile(ctx, userID, "", documents.RenderContext{Language: "en"})
	require.Error(t, err, "should fail with quota exceeded")
	require.ErrorIs(t, err, quota.ErrExceeded)

	var eventCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_usage_events WHERE user_id = ?`, userID).Scan(&eventCount))
	assert.Equal(t, 0, eventCount, "quota failure must produce no usage events")

	list, err := svc.List(ctx, userID)
	require.NoError(t, err)
	assert.Empty(t, list.Documents, "quota failure must leave no document rows")
}
