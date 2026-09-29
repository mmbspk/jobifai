package quota

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func trialTestService(t *testing.T) (*Service, *config.Store, string) {
	t.Helper()
	sqldb, err := db.Open(filepath.Join(t.TempDir(), "trial.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, keyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0,
		TrialCredits:       500,
		TrialDays:          7,
	}))

	users := auth.NewUserStore(sqldb)
	u, err := users.Create("trial@example.com", "hash", "Trial")
	require.NoError(t, err)

	sut := NewService(sqldb, cfg, users)
	require.NoError(t, sut.InitTrial(context.Background(), u.ID))
	return sut, cfg, u.ID
}

func TestTrialOverride_AllowanceDoesNotReplenishOnBurn(t *testing.T) {
	sut, cfg, userID := trialTestService(t)
	override := 1000
	require.NoError(t, sut.SaveUserOverrides(userID, domain.QuotaUserOverrides{TrialCredits: &override}))

	burn := int64(100)
	require.NoError(t, sut.RecordLLMBurn(context.Background(), userID, burn))
	row, err := sut.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(900), row.TrialRemainingCredits)

	require.NoError(t, sut.RecordLLMBurn(context.Background(), userID, burn))
	row, err = sut.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(800), row.TrialRemainingCredits)

	st, err := sut.Status(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), st.AllowanceCredits)
	assert.Equal(t, int64(200), st.UsedCredits)
	assert.Equal(t, int64(800), st.RemainingCredits)
	require.NotNil(t, st.TrialRemainingCredits)
	assert.Equal(t, int64(800), *st.TrialRemainingCredits)
	_ = cfg
}

func TestTrialOverride_IncreaseAllowancePreservesUsed(t *testing.T) {
	sut, _, userID := trialTestService(t)
	first := 1000
	require.NoError(t, sut.SaveUserOverrides(userID, domain.QuotaUserOverrides{TrialCredits: &first}))
	require.NoError(t, sut.RecordLLMBurn(context.Background(), userID, 200))

	second := 1500
	require.NoError(t, sut.SaveUserOverrides(userID, domain.QuotaUserOverrides{TrialCredits: &second}))

	row, err := sut.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(1300), row.TrialRemainingCredits)
}

func TestTrialOverride_RemoveReconcilesToGlobalDefault(t *testing.T) {
	sut, _, userID := trialTestService(t)
	override := 1000
	require.NoError(t, sut.SaveUserOverrides(userID, domain.QuotaUserOverrides{TrialCredits: &override}))
	require.NoError(t, sut.RecordLLMBurn(context.Background(), userID, 200))

	require.NoError(t, sut.SaveUserOverrides(userID, domain.QuotaUserOverrides{}))

	row, err := sut.RowForUser(userID)
	require.NoError(t, err)
	assert.Equal(t, int64(300), row.TrialRemainingCredits)

	st, err := sut.Status(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, int64(500), st.AllowanceCredits)
	assert.Equal(t, int64(200), st.UsedCredits)
}

func TestPeriodAllowanceOverride_StillApplies(t *testing.T) {
	sqldb, err := db.Open(filepath.Join(t.TempDir(), "paid.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, keyQuotaDefaults, domain.QuotaDefaults{
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
	}))

	users := auth.NewUserStore(sqldb)
	u, err := users.Create("paid@example.com", "hash", "Paid")
	require.NoError(t, err)

	sut := NewService(sqldb, cfg, users)
	require.NoError(t, sut.InitTrial(context.Background(), u.ID))
	row, err := sut.RowForUser(u.ID)
	require.NoError(t, err)
	row.Plan = domain.QuotaPlanStarter
	row.PeriodAllowanceCredits = 3000
	require.NoError(t, updateRow(sqldb, row))

	allow := int64(9000)
	require.NoError(t, sut.SaveUserOverrides(u.ID, domain.QuotaUserOverrides{PeriodAllowanceCredits: &allow}))

	row, err = sut.RowForUser(u.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(9000), row.PeriodAllowanceCredits)
}
