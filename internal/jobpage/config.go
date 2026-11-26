package jobpage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

var pilotUserPattern = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,128}$`)

func NormalizeConfig(c Config) (Config, error) {
	if c.Recovery == "" {
		c.Recovery = ModeOff
	}
	if c.Reuse == "" {
		c.Reuse = ModeOff
	}
	for _, mode := range []Mode{c.Recovery, c.Reuse} {
		if mode != ModeOff && mode != ModePilot && mode != ModeOn {
			return c, errors.New("invalid job page rollout mode")
		}
	}
	if len(c.PilotUsers) > 1000 {
		return c, errors.New("too many job page pilot users")
	}
	users := map[string]bool{}
	for _, id := range c.PilotUsers {
		id = strings.TrimSpace(id)
		if !pilotUserPattern.MatchString(id) {
			return c, errors.New("invalid job page pilot user")
		}
		users[id] = true
	}
	c.PilotUsers = nil
	for id := range users {
		c.PilotUsers = append(c.PilotUsers, id)
	}
	sort.Strings(c.PilotUsers)
	d := DefaultLimits()
	l := &c.Limits
	ints := []struct {
		value         *int
		fallback, max int
	}{{&l.MaxNodes, d.MaxNodes, 512}, {&l.MaxTextChars, d.MaxTextChars, 65536}, {&l.MaxImageBytes, d.MaxImageBytes, 1024 * 1024}, {&l.MaxDescriptionChars, d.MaxDescriptionChars, 262144}, {&l.MaxRequests, d.MaxRequests, 8}, {&l.MaxRecoveryActions, d.MaxRecoveryActions, 8}, {&l.WallClockSeconds, d.WallClockSeconds, 120}}
	for _, bound := range ints {
		if *bound.value == 0 {
			*bound.value = bound.fallback
		}
		if *bound.value < 1 || *bound.value > bound.max {
			return c, errors.New("job page limit outside safe bounds")
		}
	}
	longs := []struct {
		value         *int64
		fallback, max int64
	}{{&l.MaxTokens, d.MaxTokens, 100000}, {&l.MaxCostMicroUSD, d.MaxCostMicroUSD, 2000000}}
	for _, bound := range longs {
		if *bound.value == 0 {
			*bound.value = bound.fallback
		}
		if *bound.value < 1 || *bound.value > bound.max {
			return c, errors.New("job page limit outside safe bounds")
		}
	}
	c.Revision = ""
	raw, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	c.Revision = fmt.Sprintf("jpc1:%x", sha256.Sum256(raw))
	return c, nil
}

func (c Config) RecoveryEnabled(userID string) bool {
	return modeEnabled(c.Recovery, c.PilotUsers, userID)
}
func (c Config) ReuseEnabled(userID string) bool { return modeEnabled(c.Reuse, c.PilotUsers, userID) }
func modeEnabled(mode Mode, pilots []string, userID string) bool {
	if mode == ModeOn {
		return true
	}
	if mode != ModePilot || userID == "" {
		return false
	}
	for _, id := range pilots {
		if id == userID {
			return true
		}
	}
	return false
}

func decodeConfig(raw, revision string) (Config, error) {
	if len(raw) > 256*1024 {
		return Config{}, errors.New("invalid stored job page configuration")
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return c, errors.New("invalid stored job page configuration trailing data")
	}
	c, err := NormalizeConfig(c)
	if err != nil {
		return c, err
	}
	if c.Revision != revision {
		return c, errors.New("invalid stored job page configuration revision")
	}
	return c, nil
}

func (s *SQLStore) GetConfig(ctx context.Context) (Config, error) {
	var raw, revision string
	err := s.DB.QueryRowContext(ctx, "SELECT config_json,revision FROM job_page_config WHERE singleton=1").Scan(&raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return NormalizeConfig(Config{})
	}
	if err != nil {
		return Config{}, persistence(err)
	}
	c, err := decodeConfig(raw, revision)
	return c, persistence(err)
}

// SetConfig compares the previously observed revision in the same atomic write
// that changes the configuration. Runtime reads always load its current value.
func (s *SQLStore) SetConfig(ctx context.Context, actor, expectedRevision string, c Config) (Config, error) {
	c, err := NormalizeConfig(c)
	if err != nil {
		return c, err
	}
	defaults, _ := NormalizeConfig(Config{})
	raw, err := json.Marshal(c)
	if err != nil {
		return c, persistence(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return c, persistence(err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().UnixNano()
	res, err := tx.ExecContext(ctx, "INSERT INTO job_page_config(singleton,config_json,revision,updated_at_ns) SELECT 1,?,?,? WHERE EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin=1) AND (EXISTS(SELECT 1 FROM job_page_config WHERE singleton=1) OR ?=?) ON CONFLICT(singleton) DO UPDATE SET config_json=excluded.config_json,revision=excluded.revision,updated_at_ns=excluded.updated_at_ns WHERE job_page_config.revision=?", string(raw), c.Revision, now, actor, expectedRevision, defaults.Revision, expectedRevision)
	if err != nil {
		return c, persistence(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return c, persistence(err)
	}
	if n == 0 {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return c, err
		}
		return c, ErrConflict
	}
	if expectedRevision != c.Revision {
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_page_admin_audit(actor_user_id,action,subject_id,revision,created_at_ns) VALUES (?,'set_config','reader',?,?)", actor, c.Revision, now); err != nil {
			return c, persistence(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return c, persistence(err)
	}
	return c, nil
}

type AuditEntry struct {
	ID          int64     `json:"id"`
	ActorUserID string    `json:"actor_user_id"`
	Action      string    `json:"action"`
	SubjectID   string    `json:"subject_id"`
	Revision    string    `json:"revision,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *SQLStore) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,actor_user_id,action,subject_id,revision,created_at_ns FROM job_page_admin_audit ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, persistence(err)
	}
	defer func() { _ = rows.Close() }()
	result := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var ns int64
		if err := rows.Scan(&e.ID, &e.ActorUserID, &e.Action, &e.SubjectID, &e.Revision, &ns); err != nil {
			return nil, persistence(err)
		}
		e.CreatedAt = time.Unix(0, ns).UTC()
		result = append(result, e)
	}
	return result, persistence(rows.Err())
}
