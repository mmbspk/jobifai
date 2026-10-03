package llmreuse

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MessagePart struct {
	Role    string
	Content string
}

const (
	StatePending          = "pending"
	StateInProgress       = "in_progress"
	StateCompleted        = "completed"
	StateFailedUncertain  = "failed_uncertain"
	defaultLeaseDuration  = 3 * time.Minute
	waitPollInterval      = 200 * time.Millisecond
	maxWaitForInProgress  = 90 * time.Second
)

var (
	ErrNotConfigured     = errors.New("llm reuse store not configured")
	ErrWaitTimeout       = errors.New("llm reuse: timed out waiting for in-progress generation")
	ErrBeginContention   = errors.New("llm reuse: too many concurrent begin attempts")
	maxBeginContention   = 12
)

type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct {
	DB DB
}

// ContentFingerprint hashes user, task, and canonical message bodies (not visual/style).
func ContentFingerprint(userID, task string, msgs []MessagePart) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(userID)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(task)))
	h.Write([]byte{0})
	for _, m := range msgs {
		h.Write([]byte(strings.TrimSpace(m.Role)))
		h.Write([]byte{0})
		h.Write([]byte(strings.TrimSpace(m.Content)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// VisualIdentityHash separates presentation inputs from generation reuse scope.
func VisualIdentityHash(styleName, market, cssSnapshot string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(styleName)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(market)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(cssSnapshot)))
	return hex.EncodeToString(h.Sum(nil))
}

type BeginResult struct {
	CacheHit      bool
	Response      string
	LeaseAcquired bool
	RowID         string
}

// Begin coordinates concurrent reuse for one logical generation.
func (s *Store) Begin(ctx context.Context, userID, task, contentFP, visualHash, operationID string) (BeginResult, error) {
	if s == nil || s.DB == nil {
		return BeginResult{}, ErrNotConfigured
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = uuid.NewString()
	}
	var lastErr error
	for attempt := 0; attempt < maxBeginContention; attempt++ {
		br, err := s.beginOnce(ctx, userID, task, contentFP, visualHash, operationID)
		if err == nil {
			return br, nil
		}
		lastErr = err
		if !errors.Is(err, ErrBeginContention) {
			return BeginResult{}, err
		}
	}
	return BeginResult{}, lastErr
}

func (s *Store) beginOnce(ctx context.Context, userID, task, contentFP, visualHash, operationID string) (BeginResult, error) {
	_ = s.recoverStaleLeases(ctx)

	var existing struct {
		id, state, response string
	}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, state, response_text FROM llm_generation_cache
		WHERE user_id = ? AND task = ? AND content_fingerprint = ?`,
		userID, task, contentFP).Scan(&existing.id, &existing.state, &existing.response)
	if err == nil {
		switch existing.state {
		case StateCompleted:
			return BeginResult{CacheHit: true, Response: existing.response, RowID: existing.id}, nil
		case StateInProgress:
			resp, waitErr := s.waitForCompletion(ctx, userID, task, contentFP)
			if waitErr != nil {
				return BeginResult{}, waitErr
			}
			if resp == "" {
				return BeginResult{}, ErrBeginContention
			}
			return BeginResult{CacheHit: true, Response: resp, RowID: existing.id}, nil
		case StateFailedUncertain:
			// Provider outcome unknown after crash — caller may invoke provider; do not reuse stale body.
			leaseUntil := time.Now().UTC().Add(defaultLeaseDuration).Format(time.RFC3339)
			res, uerr := s.DB.ExecContext(ctx, `
				UPDATE llm_generation_cache SET state = ?, operation_id = ?, lease_owner = ?, lease_until = ?,
					provider_uncertain = 0, updated_at = datetime('now')
				WHERE user_id = ? AND task = ? AND content_fingerprint = ? AND state = ?`,
				StateInProgress, operationID, operationID, leaseUntil,
				userID, task, contentFP, StateFailedUncertain)
			if uerr != nil {
				return BeginResult{}, uerr
			}
			if n, _ := res.RowsAffected(); n == 1 {
				return BeginResult{LeaseAcquired: true, RowID: existing.id}, nil
			}
			return BeginResult{}, ErrBeginContention
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return BeginResult{}, err
	}

	leaseUntil := time.Now().UTC().Add(defaultLeaseDuration).Format(time.RFC3339)
	rowID := uuid.NewString()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO llm_generation_cache (
			id, user_id, task, content_fingerprint, visual_identity_hash, state,
			operation_id, lease_owner, lease_until, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(user_id, task, content_fingerprint) DO NOTHING`,
		rowID, userID, task, contentFP, visualHash, StateInProgress, operationID, operationID, leaseUntil)
	if err != nil {
		return BeginResult{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return BeginResult{}, ErrBeginContention
	}
	return BeginResult{LeaseAcquired: true, RowID: rowID}, nil
}

func (s *Store) waitForCompletion(ctx context.Context, userID, task, contentFP string) (string, error) {
	deadline := time.Now().Add(maxWaitForInProgress)
	for time.Now().Before(deadline) {
		var state, response string
		err := s.DB.QueryRowContext(ctx, `
			SELECT state, response_text FROM llm_generation_cache
			WHERE user_id = ? AND task = ? AND content_fingerprint = ?`,
			userID, task, contentFP).Scan(&state, &response)
		if err != nil {
			return "", err
		}
		switch state {
		case StateCompleted:
			return response, nil
		case StateFailedUncertain:
			return "", nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
	return "", ErrWaitTimeout
}

func (s *Store) Complete(ctx context.Context, userID, task, contentFP, response string) error {
	if s == nil || s.DB == nil {
		return ErrNotConfigured
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, response_text = ?, lease_until = NULL,
			provider_uncertain = 0, updated_at = datetime('now')
		WHERE user_id = ? AND task = ? AND content_fingerprint = ?`,
		StateCompleted, response, userID, task, contentFP)
	return err
}

func (s *Store) MarkFailedUncertain(ctx context.Context, userID, task, contentFP string) error {
	if s == nil || s.DB == nil {
		return ErrNotConfigured
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, provider_uncertain = 1, updated_at = datetime('now')
		WHERE user_id = ? AND task = ? AND content_fingerprint = ?`,
		StateFailedUncertain, userID, task, contentFP)
	return err
}

func (s *Store) recoverStaleLeases(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, provider_uncertain = 1, updated_at = datetime('now')
		WHERE state = ? AND lease_until IS NOT NULL AND lease_until < datetime('now')`,
		StateFailedUncertain, StateInProgress)
	return err
}
