package jobpage

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaderConfigDefaultsBoundsAndIndependentPilotModes(t *testing.T) {
	c, err := NormalizeConfig(Config{})
	require.NoError(t, err)
	require.Equal(t, ModeOff, c.Recovery)
	require.Equal(t, ModeOff, c.Reuse)
	require.Equal(t, DefaultLimits(), c.Limits)
	_, err = NormalizeConfig(Config{Recovery: "maybe"})
	require.Error(t, err)
	_, err = NormalizeConfig(Config{Limits: Limits{MaxNodes: 10000}})
	require.Error(t, err)
	_, err = NormalizeConfig(Config{Limits: Limits{MaxCostMicroUSD: -1}})
	require.Error(t, err)
	c, err = NormalizeConfig(Config{Recovery: ModePilot, Reuse: ModeOn, PilotUsers: []string{"reader-user", "reader-admin", "reader-user"}})
	require.NoError(t, err)
	require.Equal(t, []string{"reader-admin", "reader-user"}, c.PilotUsers)
	require.True(t, c.RecoveryEnabled("reader-user"))
	require.False(t, c.RecoveryEnabled("other-user"))
	require.True(t, c.ReuseEnabled("other-user"))
	other, err := NormalizeConfig(Config{Recovery: ModePilot, Reuse: ModeOn, PilotUsers: []string{"reader-admin", "reader-user"}, Revision: "caller-supplied"})
	require.NoError(t, err)
	require.Equal(t, c.Revision, other.Revision)
}

func TestReaderConfigAdminAtomicConcurrencyAndHotRollback(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	ctx := context.Background()
	defaults, err := store.GetConfig(ctx)
	require.NoError(t, err)
	_, err = store.SetConfig(ctx, "reader-user", defaults.Revision, Config{Recovery: ModeOn})
	require.ErrorIs(t, err, ErrForbidden)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, c := range []Config{{Recovery: ModeOn}, {Reuse: ModeOn}} {
		wg.Add(1)
		go func(c Config) {
			defer wg.Done()
			_, err := store.SetConfig(ctx, "reader-admin", defaults.Revision, c)
			errs <- err
		}(c)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
	current, err := store.GetConfig(ctx)
	require.NoError(t, err)
	require.NotEqual(t, defaults.Revision, current.Revision)
	_, err = store.SetConfig(ctx, "reader-admin", current.Revision, current)
	require.NoError(t, err)
	audit, err := store.ListAudit(ctx, 100)
	require.NoError(t, err)
	require.Len(t, audit, 1, "same normalized config is not a new audit action")
	rollback, err := store.SetConfig(ctx, "reader-admin", current.Revision, Config{})
	require.NoError(t, err)
	require.Equal(t, defaults.Revision, rollback.Revision)
	hot, err := store.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, hot.RecoveryEnabled("reader-user"))
	require.False(t, hot.ReuseEnabled("reader-user"))
}

func TestReaderConfigAuditFailureRollsBack(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	ctx := context.Background()
	c, err := store.GetConfig(ctx)
	require.NoError(t, err)
	_, err = store.DB.Exec("DROP TABLE job_page_admin_audit")
	require.NoError(t, err)
	_, err = store.SetConfig(ctx, "reader-admin", c.Revision, Config{Recovery: ModeOn})
	require.Equal(t, FailurePersistence, ErrorCode(err))
	current, err := store.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, c.Revision, current.Revision)
}
