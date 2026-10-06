package llm

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/llmreuse"
)

type renewalDB struct {
	err      error
	affected int64
}

func (d renewalDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return renewalResult(d.affected), d.err
}
func (d renewalDB) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

type renewalResult int64

func (r renewalResult) LastInsertId() (int64, error) { return 0, nil }
func (r renewalResult) RowsAffected() (int64, error) { return int64(r), nil }
func TestReuseHeartbeat_CancelsOnDatabaseFailureAndLostOwnership(t *testing.T) {
	dbErr := errors.New("renewal unavailable")
	for _, tc := range []struct {
		name string
		db   renewalDB
		want error
	}{{"database", renewalDB{err: dbErr}, dbErr}, {"lost lease", renewalDB{}, llmreuse.ErrLeaseLost}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{userID: "u", reuse: &ReuseCoordinator{Store: &llmreuse.Store{DB: tc.db}}}
			ctx, stop := c.reuseHeartbeat(context.Background(), "task", "fp", "owner", time.Millisecond)
			defer stop()
			select {
			case <-ctx.Done():
				require.ErrorIs(t, context.Cause(ctx), tc.want)
			case <-time.After(time.Second):
				t.Fatal("provider context was not canceled")
			}
		})
	}
}
