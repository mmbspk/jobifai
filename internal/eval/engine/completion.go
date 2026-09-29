package engine

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/user/jobifai/internal/eval/runmeta"
)

type runCompletion struct {
	done chan struct{}
}

var (
	runCompletionMu sync.Mutex
	runCompletions  = map[string]*runCompletion{}
)

func registerRunCompletion(id string) {
	runCompletionMu.Lock()
	defer runCompletionMu.Unlock()
	runCompletions[id] = &runCompletion{done: make(chan struct{})}
}

func completeRun(id string) {
	runCompletionMu.Lock()
	c, ok := runCompletions[id]
	runCompletionMu.Unlock()
	if ok {
		close(c.done)
	}
}

// WaitForRun blocks until the async ExecuteRun goroutine for id finishes or ctx times out.
func WaitForRun(ctx context.Context, id string) error {
	runCompletionMu.Lock()
	c, ok := runCompletions[id]
	runCompletionMu.Unlock()
	if !ok {
		return fmt.Errorf("no async run registered for %q (CreateRun did not start background execution?)", id)
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunDiagnostics summarizes run state for test failures.
func RunDiagnostics(db *sql.DB, id string) string {
	var status, errMsg string
	var planned, completed int
	var spent int64
	_ = db.QueryRow(`SELECT status, COALESCE(error_message,''), cases_planned, cases_completed, COALESCE(actual_cost_usd_micro,0) FROM model_eval_runs WHERE id=?`, id).
		Scan(&status, &errMsg, &planned, &completed, &spent)
	var recs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM model_eval_recommendations WHERE eval_run_id=?`, id).Scan(&recs)
	return fmt.Sprintf("status=%q error_message=%q cases_planned=%d cases_completed=%d actual_cost_usd_micro=%d recommendation_count=%d",
		status, errMsg, planned, completed, spent, recs)
}

// WaitForRunWithDiagnostics waits for async completion with a timeout and returns diagnostics on failure.
func WaitForRunWithDiagnostics(db *sql.DB, id string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := WaitForRun(ctx, id); err != nil {
		return fmt.Errorf("%w: %s", err, RunDiagnostics(db, id))
	}
	var status string
	var planned, completed int
	if err := db.QueryRow(`SELECT status, cases_planned, cases_completed FROM model_eval_runs WHERE id=?`, id).
		Scan(&status, &planned, &completed); err != nil {
		return fmt.Errorf("load run after async wait: %w: %s", err, RunDiagnostics(db, id))
	}
	switch status {
	case runmeta.StatusCompleted, runmeta.StatusFailed, runmeta.StatusCancelled, runmeta.StatusBudgetExhausted:
	default:
		return fmt.Errorf("run still non-terminal status %q: %s", status, RunDiagnostics(db, id))
	}
	return nil
}
