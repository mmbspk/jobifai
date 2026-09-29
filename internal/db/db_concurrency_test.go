package db_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func TestOpen_ConcurrentReadsDuringWrite(t *testing.T) {
	sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "conc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	_, err = sqldb.Exec(`CREATE TABLE IF NOT EXISTS conc_probe (id INTEGER PRIMARY KEY, n INTEGER)`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, err := sqldb.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		defer func() { _ = tx.Rollback() }()
		_, _ = tx.Exec(`INSERT INTO conc_probe (n) VALUES (1)`)
		time.Sleep(200 * time.Millisecond)
		_ = tx.Commit()
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			var n int
			_ = sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM conc_probe`).Scan(&n)
			time.Sleep(10 * time.Millisecond)
		}
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent read/write timed out")
	}
}
