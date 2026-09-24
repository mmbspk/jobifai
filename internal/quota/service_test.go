package quota

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func TestService_TrialCreditsAndExpiry(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	cfg := config.NewStore(sqldb)
	require.NoError(t, cfg.Set(domain.SystemUserID, keyQuotaDefaults, domain.QuotaDefaults{
		TrialCredits:       100,
		TrialDays:          7,
		EnforcementDefault: true,
		CreditsPerUSD:      1000,
		ServiceMarkup:      0.5,
	}))

	users := auth.NewUserStore(sqldb)
	u, err := users.Create("trial@example.com", "hash", "Trial")
	require.NoError(t, err)

	sut := NewService(sqldb, cfg, users)
	require.NoError(t, sut.InitTrial(context.Background(), u.ID))

	burn := BurnCredits(loadDefaults(cfg), "claude-sonnet-4-6", 5000, 2000)
	require.NoError(t, sut.RecordLLM(context.Background(), u.ID, "claude-sonnet-4-6", 5000, 2000))
	_ = burn

	row, err := sut.RowForUser(u.ID)
	require.NoError(t, err)
	require.Less(t, row.TrialRemainingCredits, int64(100))

	// Force expiry
	row.TrialEndsAt = ptrTime(time.Now().Add(-time.Hour))
	require.NoError(t, updateRow(sqldb, row))
	err = sut.BeforeLLM(context.Background(), u.ID, "claude-sonnet-4-6", 100, 100)
	require.Error(t, err)
}

func ptrTime(t time.Time) *time.Time { return &t }
