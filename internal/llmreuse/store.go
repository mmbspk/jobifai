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
	StatePending         = "pending"
	StateInProgress      = "in_progress"
	StateCompleted       = "completed"
	StateFailedUncertain = "failed_uncertain"
	waitPollInterval     = 200 * time.Millisecond
	maxWaitForInProgress = 90 * time.Second
)

var (
	ErrLeaseLost       = errors.New("llm reuse: lease ownership lost")
	ErrNotConfigured   = errors.New("llm reuse store not configured")
	ErrWaitTimeout     = errors.New("llm reuse: timed out waiting for in-progress generation")
	ErrBeginContention = errors.New("llm reuse: too many concurrent begin attempts")
	maxBeginContention = 12
)

type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct {
	DB DB
}

// ContentFingerprint hashes user, task, effective generation config, and message bodies.
func ContentFingerprint(userID, task, provider, model, effort string, maxTokens int, msgs []MessagePart) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(userID)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(task)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(provider)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(model)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(effort)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(itoa(maxTokens))))
	h.Write([]byte{0})
	for _, m := range msgs {
		h.Write([]byte(strings.TrimSpace(m.Role)))
		h.Write([]byte{0})
		h.Write([]byte(strings.TrimSpace(m.Content)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

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
	OperationID   string
	LeaseOwner    string
}

func newLeaseToken() string {
	return uuid.NewString()
}

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

	var existing struct {
		id, state, response, storedOp string
		expired                       bool
	}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, state, response_text, operation_id, COALESCE(lease_until < datetime('now'), 0) FROM llm_generation_cache
		WHERE user_id = ? AND task = ? AND content_fingerprint = ?`,
		userID, task, contentFP).Scan(&existing.id, &existing.state, &existing.response, &existing.storedOp, &existing.expired)
	if err == nil {
		opID := strings.TrimSpace(existing.storedOp)
		if opID == "" {
			opID = operationID
		}
		switch existing.state {
		case StateCompleted:
			return BeginResult{CacheHit: true, Response: existing.response, RowID: existing.id, OperationID: opID}, nil
		case StateInProgress:
			if existing.expired {
				if err := s.recoverStaleLease(ctx, userID, task, contentFP); err != nil {
					return BeginResult{}, err
				}
				return BeginResult{}, ErrBeginContention
			}
			resp, waitErr := s.waitForCompletion(ctx, userID, task, contentFP)
			if waitErr != nil {
				return BeginResult{}, waitErr
			}
			if resp == "" {
				return BeginResult{}, ErrBeginContention
			}
			return BeginResult{CacheHit: true, Response: resp, RowID: existing.id, OperationID: opID}, nil
		case StateFailedUncertain:
			lease := newLeaseToken()
			res, uerr := s.execContext(ctx, `
				UPDATE llm_generation_cache SET state = ?, lease_owner = ?, lease_until = datetime('now', '+3 minutes'),
					provider_uncertain = 0, updated_at = datetime('now')
				WHERE user_id = ? AND task = ? AND content_fingerprint = ? AND state = ?`,
				StateInProgress, lease, userID, task, contentFP, StateFailedUncertain)
			if uerr != nil {
				return BeginResult{}, uerr
			}
			if n, _ := res.RowsAffected(); n == 1 {
				return BeginResult{LeaseAcquired: true, RowID: existing.id, OperationID: opID, LeaseOwner: lease}, nil
			}
			return BeginResult{}, ErrBeginContention
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return BeginResult{}, err
	}

	lease := newLeaseToken()
	rowID := uuid.NewString()
	res, err := s.execContext(ctx, `
		INSERT INTO llm_generation_cache (
			id, user_id, task, content_fingerprint, visual_identity_hash, state,
			operation_id, lease_owner, lease_until, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now', '+3 minutes'), datetime('now'))
		ON CONFLICT(user_id, task, content_fingerprint) DO NOTHING`,
		rowID, userID, task, contentFP, visualHash, StateInProgress, operationID, lease)
	if err != nil {
		return BeginResult{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return BeginResult{}, ErrBeginContention
	}
	return BeginResult{LeaseAcquired: true, RowID: rowID, OperationID: operationID, LeaseOwner: lease}, nil
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

func (s *Store) RenewLease(ctx context.Context, userID, task, contentFP, leaseOwner string) error {
	if s == nil || s.DB == nil {
		return ErrNotConfigured
	}
	res, err := s.execContext(ctx, `
		UPDATE llm_generation_cache SET lease_until = datetime('now', '+3 minutes'), updated_at = datetime('now')
		WHERE user_id = ? AND task = ? AND content_fingerprint = ? AND lease_owner = ? AND state = ?`,
		userID, task, contentFP, leaseOwner, StateInProgress)
	return requireLeaseUpdate(res, err)
}

func (s *Store) Complete(ctx context.Context, userID, task, contentFP, leaseOwner, response string) error {
	if s == nil || s.DB == nil {
		return ErrNotConfigured
	}
	res, err := s.execContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, response_text = ?, lease_until = NULL,
			provider_uncertain = 0, updated_at = datetime('now')
		WHERE user_id = ? AND task = ? AND content_fingerprint = ? AND lease_owner = ? AND state = ?`,
		StateCompleted, response, userID, task, contentFP, leaseOwner, StateInProgress)
	return requireLeaseUpdate(res, err)
}

func (s *Store) MarkFailedUncertain(ctx context.Context, userID, task, contentFP, leaseOwner string) error {
	if s == nil || s.DB == nil {
		return ErrNotConfigured
	}
	res, err := s.execContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, provider_uncertain = 1, updated_at = datetime('now')
		WHERE user_id = ? AND task = ? AND content_fingerprint = ? AND lease_owner = ? AND state = ?`,
		StateFailedUncertain, userID, task, contentFP, leaseOwner, StateInProgress)
	return requireLeaseUpdate(res, err)
}

func requireLeaseUpdate(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) recoverStaleLease(ctx context.Context, userID, task, contentFP string) error {
	_, err := s.execContext(ctx, `
		UPDATE llm_generation_cache SET state = ?, provider_uncertain = 1, updated_at = datetime('now')
		WHERE state = ? AND lease_until IS NOT NULL AND lease_until < datetime('now') AND user_id = ? AND task = ? AND content_fingerprint = ?`,
		StateFailedUncertain, StateInProgress, userID, task, contentFP)
	return err
}

// Retry transient SQLite writer contention without starting another provider call.
func (s *Store) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err := s.DB.ExecContext(ctx, query, args...)
		var coded interface{ Code() int }
		if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) || time.Now().After(deadline) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
