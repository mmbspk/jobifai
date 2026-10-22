package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/user/jobifai/internal/domain"
	jobws "github.com/user/jobifai/internal/ws"
)

// runEntry tracks the active run ID for a user inside RunLogSink.
type runEntry struct {
	runID string
}

// RunLogSink is an io.Writer that persists dashboard-visible bot log lines to
// the bot_run_logs table. It is wired into the zerolog MultiLevelWriter chain
// so every log line emitted through log.Logger passes through it.
//
// Each bot run is bounded by Register/Unregister calls from Manager. Lines are
// only stored when a run is active and their log_id + automation_run_id fields
// match the registered run. The 500-row per-user cap is maintained transactionally.
type RunLogSink struct {
	db   *sql.DB
	// sinkLog is wired directly to stderr and is NOT in its own writer chain,
	// preventing recursive log calls.
	sinkLog zerolog.Logger

	mu           sync.RWMutex
	runs         map[string]*runEntry // key = userID

	// userMutexes serializes per-user DB operations (Register + Write) so that
	// a new registration cannot race with an in-flight write for the same user.
	userMutexes sync.Map
}

// NewRunLogSink creates a sink backed by db. sinkLog should write to stderr only.
func NewRunLogSink(db *sql.DB) *RunLogSink {
	sinkLog := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("component", "run_log_sink").Logger()
	return &RunLogSink{
		db:      db,
		sinkLog: sinkLog,
		runs:    make(map[string]*runEntry),
	}
}

func (s *RunLogSink) userMu(userID string) *sync.Mutex {
	v, _ := s.userMutexes.LoadOrStore(userID, &sync.Mutex{})
	return v.(*sync.Mutex) //nolint:forcetypeassert
}

// Register atomically clears old logs for userID and records the new runID.
// Derives its timeout from ctx so cancellation of the start request is honoured.
func (s *RunLogSink) Register(ctx context.Context, userID, runID string) error {
	mu := s.userMu(userID)
	mu.Lock()
	defer mu.Unlock()

	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(tctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(tctx, `DELETE FROM bot_run_logs WHERE user_id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(tctx,
		`INSERT OR REPLACE INTO bot_current_run (user_id, run_id) VALUES (?,?)`,
		userID, runID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	s.mu.Lock()
	s.runs[userID] = &runEntry{runID: runID}
	s.mu.Unlock()
	return nil
}

// Unregister removes the active run entry for userID, but only when runID still
// matches. A later run's entry is never evicted by a delayed cleanup goroutine.
func (s *RunLogSink) Unregister(userID, runID string) {
	s.mu.Lock()
	if e, ok := s.runs[userID]; ok && e.runID == runID {
		delete(s.runs, userID)
	}
	s.mu.Unlock()
}

// Write implements io.Writer. It is called for every zerolog line that passes
// through the MultiLevelWriter chain and persists dashboard-visible lines that
// carry log_id + automation_run_id fields matching the active run.
func (s *RunLogSink) Write(p []byte) (int, error) {
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		return len(p), nil
	}
	userID, _ := m["user_id"].(string)
	automationRunID, _ := m["automation_run_id"].(string)
	logID, _ := m["log_id"].(string)
	if userID == "" || automationRunID == "" || logID == "" {
		return len(p), nil
	}
	if !jobws.VisibleOnDashboard(m) {
		return len(p), nil
	}

	// Quick pre-check before acquiring the per-user mutex.
	s.mu.RLock()
	entry, ok := s.runs[userID]
	validPre := ok && entry.runID == automationRunID
	s.mu.RUnlock()
	if !validPre {
		return len(p), nil
	}

	// Serialize per-user DB work; re-validate under the per-user mutex so that
	// a Register racing between pre-check and here cannot inject a stale row.
	mu := s.userMu(userID)
	mu.Lock()
	defer mu.Unlock()

	s.mu.RLock()
	entry, ok = s.runs[userID]
	valid := ok && entry.runID == automationRunID
	s.mu.RUnlock()
	if !valid {
		return len(p), nil
	}

	tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	level, _ := m["level"].(string)
	msg := extractLogMsg(m)

	// Trim-and-insert in one transaction to maintain the exact 500-row cap.
	tx, err := s.db.BeginTx(tctx, nil)
	if err != nil {
		s.sinkLog.Error().Err(err).Str("user_id", userID).Msg("begin tx")
		return len(p), nil
	}
	if _, err = tx.ExecContext(tctx,
		`DELETE FROM bot_run_logs
		 WHERE user_id = ? AND seq IN (
		   SELECT seq FROM bot_run_logs WHERE user_id = ? ORDER BY seq ASC
		   LIMIT MAX(0, (SELECT COUNT(*) FROM bot_run_logs WHERE user_id = ?) - 499)
		 )`,
		userID, userID, userID); err != nil {
		_ = tx.Rollback()
		s.sinkLog.Error().Err(err).Str("user_id", userID).Msg("trim")
		return len(p), nil
	}
	if _, err = tx.ExecContext(tctx,
		`INSERT INTO bot_run_logs (id, user_id, run_id, level, message, raw_json) VALUES (?,?,?,?,?,?)`,
		logID, userID, automationRunID, level, msg, string(p)); err != nil {
		_ = tx.Rollback()
		s.sinkLog.Error().Err(err).Str("user_id", userID).Msg("insert")
		return len(p), nil
	}
	if err = tx.Commit(); err != nil {
		s.sinkLog.Error().Err(err).Str("user_id", userID).Msg("commit")
	}
	return len(p), nil
}

// LatestRun returns the run ID and log lines for userID in a single read
// transaction to avoid the race where a new registration between two separate
// queries would return the new run ID paired with stale log rows.
func (s *RunLogSink) LatestRun(ctx context.Context, userID string) (domain.BotRunLogs, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.BotRunLogs{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var runID string
	err = tx.QueryRowContext(ctx,
		`SELECT run_id FROM bot_current_run WHERE user_id = ?`, userID).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.BotRunLogs{Lines: []domain.BotRunLog{}}, nil
	}
	if err != nil {
		return domain.BotRunLogs{}, err
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, level, message, raw_json, created_at
		 FROM bot_run_logs WHERE user_id = ? AND run_id = ?
		 ORDER BY seq ASC LIMIT 500`,
		userID, runID)
	if err != nil {
		return domain.BotRunLogs{}, err
	}
	defer rows.Close()

	lines := make([]domain.BotRunLog, 0)
	for rows.Next() {
		var l domain.BotRunLog
		if err = rows.Scan(&l.ID, &l.Level, &l.Message, &l.RawJSON, &l.CreatedAt); err != nil {
			return domain.BotRunLogs{}, err
		}
		lines = append(lines, l)
	}
	if err = rows.Err(); err != nil {
		return domain.BotRunLogs{}, err
	}
	return domain.BotRunLogs{RunID: runID, Lines: lines}, nil
}

// extractLogMsg returns the human-readable message from a zerolog entry map.
func extractLogMsg(m map[string]any) string {
	for _, k := range []string{"message", "msg", "m"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// logIDHook appends a unique log_id to every zerolog event emitted through a
// bot-scoped logger. The ID is shared across the WS broadcaster (which passes
// raw JSON) and the RunLogSink (which reads it for the row PK), enabling the
// frontend to deduplicate lines that arrive via both channels.
type logIDHook struct{}

func (logIDHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("log_id", uuid.NewString())
}
