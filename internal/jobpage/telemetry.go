package jobpage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const captureRetention = 7 * 24 * time.Hour
const maxOwnedCaptures = 20

type ReadEvent struct {
	ID        int64       `json:"id"`
	UserID    string      `json:"user_id"`
	RunID     string      `json:"run_id,omitempty"`
	AttemptID string      `json:"attempt_id,omitempty"`
	RecordID  string      `json:"record_id,omitempty"`
	Scope     Scope       `json:"scope"`
	Source    Source      `json:"source,omitempty"`
	Failure   FailureCode `json:"failure_code,omitempty"`
	RecipeID  string      `json:"recipe_id,omitempty"`
	Metrics   Metrics     `json:"metrics"`
	CreatedAt time.Time   `json:"created_at"`
}

type PrivateCapture struct {
	ID           int64
	UserID       string
	ReadEventID  int64
	SnapshotJSON []byte
	Screenshot   []byte
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

func validEvent(e ReadEvent) bool {
	for _, value := range []string{e.UserID, e.RunID, e.AttemptID, e.RecordID} {
		if value != "" && !pilotUserPattern.MatchString(value) {
			return false
		}
	}
	if e.UserID == "" {
		return false
	}
	_, scope, err := NormalizeTarget(Target{Platform: e.Scope.Platform, URL: e.Scope.Origin + map[string]string{"linkedin": "/jobs/view/1/", "seek": "/job/1"}[e.Scope.Platform]})
	if err != nil || scope != e.Scope {
		return false
	}
	if e.Source != "" && e.Source != SourceDeterministic && e.Source != SourceRecovery && e.Source != SourceRecipe && e.Source != SourceModel {
		return false
	}
	if e.RecipeID != "" && (!strings.HasPrefix(e.RecipeID, "jpr1:") || len(e.RecipeID) != 69) {
		return false
	}
	switch e.Failure {
	case "", FailureIdentity, FailureDrift, FailureOrigin, FailureAuth, FailureExpired, FailureMissing, FailureThin, FailureAmbiguous, FailureStale, FailureBrowser, FailurePlan, FailureVerification, FailureBudget, FailurePersistence, FailureProvider, FailureQuota, FailureCanceled, FailureDeadline:
	default:
		return false
	}
	m := e.Metrics
	return m.DeterministicReads >= 0 && m.RecipeHits >= 0 && m.RecoveryActions >= 0 && m.ProposalRequests >= 0 && m.VerificationRequests >= 0 && m.HTTPAttempts >= 0 && m.InputTokens >= 0 && m.OutputTokens >= 0 && m.CostMicroUSD >= 0 && m.BrowserMS >= 0 && m.LatencyMS >= 0
}

// RecordRead keeps public accounting separate from optional owner-only capture
// evidence. Both inserts and bounded retention complete in one transaction.
func (s *SQLStore) RecordRead(ctx context.Context, e ReadEvent, capture *Snapshot) (int64, error) {
	if !validEvent(e) {
		return 0, errors.New("invalid job page reading event")
	}
	var snapshotJSON []byte
	if capture != nil {
		config, err := s.GetConfig(ctx)
		if err != nil {
			return 0, err
		}
		if !config.PrivateCaptures {
			return 0, errors.New("job page private captures are disabled")
		}
		if capture.Target.UserID != e.UserID || capture.Scope != e.Scope {
			return 0, ErrForbidden
		}
		if err := ValidateSnapshot(*capture, config.Limits); err != nil {
			return 0, err
		}
		snapshotJSON, err = json.Marshal(capture)
		if err != nil {
			return 0, persistence(err)
		}
		if len(snapshotJSON) > 256*1024 || len(capture.Screenshot) > config.Limits.MaxImageBytes {
			return 0, &ReadError{Code: FailureBudget}
		}
	}
	raw, err := json.Marshal(e.Metrics)
	if err != nil {
		return 0, persistence(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, persistence(err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, "INSERT INTO job_page_read_events(user_id,run_id,attempt_id,record_id,platform,origin,source,failure_code,recipe_id,metrics_json,created_at_ns) VALUES (?,?,?,?,?,?,?,?,?,?,?)", e.UserID, e.RunID, e.AttemptID, e.RecordID, e.Scope.Platform, e.Scope.Origin, e.Source, e.Failure, e.RecipeID, string(raw), now.UnixNano())
	if err != nil {
		return 0, persistence(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, persistence(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM job_page_private_captures WHERE expires_at_ns<=?", now.UnixNano()); err != nil {
		return 0, persistence(err)
	}
	if capture != nil {
		// Recheck opt-in after the transaction's first write acquires ownership of
		// the SQLite writer. A concurrent hot rollback cannot race this insert.
		var configJSON, configRevision string
		if err = tx.QueryRowContext(ctx, "SELECT config_json,revision FROM job_page_config WHERE singleton=1").Scan(&configJSON, &configRevision); err != nil {
			return 0, persistence(err)
		}
		current, configErr := decodeConfig(configJSON, configRevision)
		if configErr != nil {
			return 0, persistence(configErr)
		}
		if !current.PrivateCaptures {
			return 0, errors.New("job page private captures are disabled")
		}
		if err = ValidateSnapshot(*capture, current.Limits); err != nil {
			return 0, err
		}
		screenshot := capture.Screenshot
		if screenshot == nil {
			screenshot = []byte{}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_page_private_captures(user_id,read_event_id,snapshot_json,screenshot,created_at_ns,expires_at_ns) VALUES (?,?,?,?,?,?)", e.UserID, id, snapshotJSON, screenshot, now.UnixNano(), now.Add(captureRetention).UnixNano()); err != nil {
			return 0, persistence(err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM job_page_private_captures WHERE user_id=? AND id NOT IN (SELECT id FROM job_page_private_captures WHERE user_id=? ORDER BY created_at_ns DESC,id DESC LIMIT ?)", e.UserID, e.UserID, maxOwnedCaptures); err != nil {
			return 0, persistence(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, persistence(err)
	}
	return id, nil
}

func (s *SQLStore) PurgePrivateCaptures(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM job_page_private_captures WHERE expires_at_ns<=?", time.Now().UTC().UnixNano())
	return persistence(err)
}

func (s *SQLStore) ListReadEvents(ctx context.Context, userID string, limit int) ([]ReadEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,user_id,run_id,attempt_id,record_id,platform,origin,source,failure_code,recipe_id,metrics_json,created_at_ns FROM job_page_read_events WHERE (?='' OR user_id=?) ORDER BY id DESC LIMIT ?", userID, userID, limit)
	if err != nil {
		return nil, persistence(err)
	}
	defer func() { _ = rows.Close() }()
	result := []ReadEvent{}
	for rows.Next() {
		var e ReadEvent
		var raw string
		var ns int64
		if err := rows.Scan(&e.ID, &e.UserID, &e.RunID, &e.AttemptID, &e.RecordID, &e.Scope.Platform, &e.Scope.Origin, &e.Source, &e.Failure, &e.RecipeID, &raw, &ns); err != nil {
			return nil, persistence(err)
		}
		if err := json.Unmarshal([]byte(raw), &e.Metrics); err != nil {
			return nil, persistence(err)
		}
		e.Scope.PageType = "job_listing"
		e.Scope.SnapshotVersion = SnapshotVersion
		e.CreatedAt = time.Unix(0, ns).UTC()
		result = append(result, e)
	}
	return result, persistence(rows.Err())
}

// GetPrivateCapture enforces owner/admin access inside storage. It is deliberately
// absent from normal event lists and from the reusable recipe API.
func (s *SQLStore) GetPrivateCapture(ctx context.Context, actor string, id int64) (PrivateCapture, error) {
	var c PrivateCapture
	var created, expires int64
	err := s.DB.QueryRowContext(ctx, "SELECT id,user_id,read_event_id,snapshot_json,screenshot,created_at_ns,expires_at_ns FROM job_page_private_captures WHERE id=? AND expires_at_ns>? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin=1))", id, time.Now().UTC().UnixNano(), actor, actor).Scan(&c.ID, &c.UserID, &c.ReadEventID, &c.SnapshotJSON, &c.Screenshot, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrForbidden
	}
	if err != nil {
		return c, persistence(err)
	}
	c.CreatedAt, c.ExpiresAt = time.Unix(0, created).UTC(), time.Unix(0, expires).UTC()
	return c, nil
}
