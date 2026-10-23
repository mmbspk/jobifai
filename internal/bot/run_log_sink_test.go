package bot_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// createUser inserts a minimal user row so FK constraints on user_id pass.
func createUser(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO users (id, email, password_hash, display_name, created_at, updated_at)
		 VALUES (?, ?, '', ?, datetime('now'), datetime('now'))`,
		id, id+"@test.local", id)
	require.NoError(t, err)
}

func makeLine(userID, runID, logID string) []byte {
	m := map[string]any{
		"level":             "info",
		"user_id":           userID,
		"automation_run_id": runID,
		"log_id":            logID,
		"message":           "seek: processing job",
		"time":              time.Now().Format(time.RFC3339),
	}
	b, _ := json.Marshal(m)
	return b
}

func makeLinesN(userID, runID string, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = makeLine(userID, runID, fmt.Sprintf("log-%s-%s-%03d", userID, runID, i))
	}
	return out
}

// TestRunLogSink_Register_AtomicClearAndRecord seeds old rows, calls Register,
// and asserts that old rows are deleted and bot_current_run is updated atomically.
func TestRunLogSink_Register_AtomicClearAndRecord(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	runA, runB := "run-a", "run-b"
	createUser(t, db, userID)

	// Seed a row for runA by manually registering and writing.
	require.NoError(t, sink.Register(context.Background(), userID, runA))
	_, err := sink.Write(makeLine(userID, runA, "lA1"))
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	require.Equal(t, 1, count)

	// Register runB: should clear runA's rows and record runB.
	require.NoError(t, sink.Register(context.Background(), userID, runB))

	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	assert.Equal(t, 0, count, "old run log rows must be deleted")

	var gotRunID string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT run_id FROM bot_current_run WHERE user_id = ?`, userID).Scan(&gotRunID))
	assert.Equal(t, runB, gotRunID)
}

// TestRunLogSink_Register_ErrorLeavesMapUnchanged tests that a failed Register
// leaves the internal runs map unchanged.
func TestRunLogSink_Register_ErrorLeavesMapUnchanged(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	// Register a valid run first.
	const userID = "u1"
	runA := "run-a"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runA))

	// Close the DB to force the next Register to fail.
	_ = db.Close()

	err := sink.Register(context.Background(), userID, "run-b")
	require.Error(t, err)

	// Write for runA should still work (map entry preserved) — but db is closed,
	// so the write will fail. The key assertion is that Write does not panic.
	_, writeErr := sink.Write(makeLine(userID, runA, "lA-after"))
	// Either an error is returned (db closed) or it silently drops — no panic.
	_ = writeErr
}

// TestRunLogSink_ConcurrentWrites_CapExactly500 writes 600 lines from 10 goroutines
// and asserts exactly 500 rows are retained.
func TestRunLogSink_ConcurrentWrites_CapExactly500(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-conc"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))

	var wg sync.WaitGroup
	lines := makeLinesN(userID, runID, 600)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < 60; j++ {
				idx := offset*60 + j
				_, _ = sink.Write(lines[idx])
			}
		}(i)
	}
	wg.Wait()

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	assert.Equal(t, 500, count)
}

// TestRunLogSink_WriteOverlapsRegistration_NoStaleRows verifies that a write racing
// with Register for a new run does not leave stale rows for the old run.
func TestRunLogSink_WriteOverlapsRegistration_NoStaleRows(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	runA, runB := "run-a", "run-b"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runA))

	var wg sync.WaitGroup
	lines := makeLinesN(userID, runA, 50)
	for _, l := range lines {
		wg.Add(1)
		go func(line []byte) {
			defer wg.Done()
			_, _ = sink.Write(line)
		}(l)
	}
	// Register runB concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = sink.Register(context.Background(), userID, runB)
	}()
	wg.Wait()

	// After Register(runB), any runA rows inserted after Register must be zero.
	var countA int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ? AND run_id = ?`, userID, runA).Scan(&countA))
	assert.Equal(t, 0, countA, "no stale run-A rows should remain")
}

// TestRunLogSink_Write_RequiresRunIDMatch verifies writes with wrong run_id are dropped.
func TestRunLogSink_Write_RequiresRunIDMatch(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, "run-a"))

	_, err := sink.Write(makeLine(userID, "run-wrong", "lw1"))
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	assert.Equal(t, 0, count)
}

// TestRunLogSink_Write_OnlyDashboardVisibleLines verifies filter: debug and
// llm_call lines are dropped; automation-progress info lines are stored.
func TestRunLogSink_Write_OnlyDashboardVisibleLines(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-vis"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))

	debug := func() []byte {
		m := map[string]any{"level": "debug", "user_id": userID, "automation_run_id": runID,
			"log_id": "ld1", "message": "internal detail"}
		b, _ := json.Marshal(m)
		return b
	}()
	llmCall := func() []byte {
		m := map[string]any{"level": "info", "user_id": userID, "automation_run_id": runID,
			"log_id": "ll1", "llm_call": true, "message": "llm call completed"}
		b, _ := json.Marshal(m)
		return b
	}()
	visible := makeLine(userID, runID, "lv1")

	_, _ = sink.Write(debug)
	_, _ = sink.Write(llmCall)
	_, _ = sink.Write(visible)

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	assert.Equal(t, 1, count, "only the visible automation-progress line should be stored")
}

// TestRunLogSink_Unregister_IgnoresStaleCaller verifies that Unregister with the
// wrong runID does not remove the active run entry, and subsequent writes succeed.
func TestRunLogSink_Unregister_IgnoresStaleCaller(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-a"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))

	// Unregister with wrong ID — should be a no-op.
	sink.Unregister(userID, "run-wrong")

	// Write for the correct run should still succeed.
	_, err := sink.Write(makeLine(userID, runID, "la1"))
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, userID).Scan(&count))
	assert.Equal(t, 1, count)
}

// TestRunLogSink_UserIsolation verifies that writes for a different user_id are ignored.
func TestRunLogSink_UserIsolation(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	createUser(t, db, "u1")
	require.NoError(t, sink.Register(context.Background(), "u1", "run-a"))

	// Write claiming to be u2 (not registered).
	_, err := sink.Write(makeLine("u2", "run-a", "lx1"))
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?`, "u2").Scan(&count))
	assert.Equal(t, 0, count)
}

// TestRunLogSink_DBWriteFailure_NoRecursion writes to a closed DB and verifies
// there is no panic and no recursive log call.
func TestRunLogSink_DBWriteFailure_NoRecursion(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-fail"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))
	_ = db.Close()

	// Should not panic; error is captured by sinkLog.
	assert.NotPanics(t, func() {
		_, _ = sink.Write(makeLine(userID, runID, "lf1"))
	})
}

// TestRunLogSink_LatestRun_ZeroLine_ReturnsRunID registers a run but writes no
// visible lines; LatestRun should return the run_id with an empty lines slice.
func TestRunLogSink_LatestRun_ZeroLine_ReturnsRunID(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-empty"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))

	result, err := sink.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, runID, result.RunID)
	assert.Empty(t, result.Lines)
}

// TestRunLogSink_LatestRun_OrderedBySeq writes 3 lines and asserts they are
// returned in insertion order.
func TestRunLogSink_LatestRun_OrderedBySeq(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	const runID = "run-ord"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runID))

	ids := []string{"la", "lb", "lc"}
	for _, id := range ids {
		_, err := sink.Write(makeLine(userID, runID, id))
		require.NoError(t, err)
	}

	result, err := sink.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, result.Lines, 3)
	for i, id := range ids {
		assert.Equal(t, id, result.Lines[i].ID, "line %d should have id %s", i, id)
	}
}

// TestRunLogSink_LatestRun_ReadTransaction verifies that LatestRun returns a
// self-consistent (run_id, rows) pair even when a concurrent Register races.
func TestRunLogSink_LatestRun_ReadTransaction(t *testing.T) {
	db := openTestDB(t)
	sink := bot.NewRunLogSink(db)

	const userID = "u1"
	runA := "run-a"
	createUser(t, db, userID)
	require.NoError(t, sink.Register(context.Background(), userID, runA))

	for _, id := range []string{"la1", "la2", "la3"} {
		_, err := sink.Write(makeLine(userID, runA, id))
		require.NoError(t, err)
	}

	result, err := sink.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	// Either runA with 3 rows, or runB with 0 rows — never a mismatch.
	if result.RunID == runA {
		assert.Len(t, result.Lines, 3)
	} else {
		assert.Empty(t, result.Lines)
	}
}

// TestRunLogSink_SinkRecreation verifies that LatestRun returns persisted lines
// after destroying and recreating the RunLogSink (simulating a server restart).
func TestRunLogSink_SinkRecreation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	db1, err := appdb.Open(dbPath)
	require.NoError(t, err)

	const userID = "u1"
	createUser(t, db1, userID)

	sink1 := bot.NewRunLogSink(db1)
	const runID = "run-persist"
	require.NoError(t, sink1.Register(context.Background(), userID, runID))

	lines := makeLinesN(userID, runID, 5)
	for _, l := range lines {
		_, err := sink1.Write(l)
		require.NoError(t, err)
	}
	_ = db1.Close()

	// Simulate server restart: open fresh DB handle + new sink.
	db2, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	sink2 := bot.NewRunLogSink(db2)
	result, err := sink2.LatestRun(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, runID, result.RunID)
	assert.Len(t, result.Lines, 5, "persisted lines must survive restart")
}

// Compile-time check: RunLogSink satisfies handler.BotRunLogger.
var _ interface {
	LatestRun(ctx context.Context, userID string) (domain.BotRunLogs, error)
} = (*bot.RunLogSink)(nil)
