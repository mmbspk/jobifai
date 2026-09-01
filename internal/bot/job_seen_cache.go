package bot

import (
	"database/sql"
	"sync"
)

// jobSeenKind records why a job id is considered "already seen".
type jobSeenKind int

const (
	seenApplied jobSeenKind = iota
	seenSkipped
	seenPendingReview
	seenApproved
)

func (k jobSeenKind) reason() string {
	switch k {
	case seenApplied:
		return "already applied"
	case seenSkipped:
		return "in skipped list"
	case seenPendingReview:
		return "in Top Matches"
	case seenApproved:
		return "in approved queue"
	default:
		return ""
	}
}

// jobSeenCache holds job ids the user has already applied to, skipped, queued
// for review, or approved — loaded once per bot run to avoid per-job DB lookups.
type jobSeenCache struct {
	mu   sync.RWMutex
	byID map[string]jobSeenKind
}

func newJobSeenCache() *jobSeenCache {
	return &jobSeenCache{byID: make(map[string]jobSeenKind)}
}

// load reads all known job ids for userID into memory (four queries total).
func (c *jobSeenCache) load(db *sql.DB, userID string) (int, error) {
	if db == nil {
		return 0, nil
	}
	next := make(map[string]jobSeenKind)

	loadIDs := func(query string, kind jobSeenKind, onlyIfAbsent bool) error {
		rows, err := db.Query(query, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if id == "" {
				continue
			}
			if onlyIfAbsent {
				if _, exists := next[id]; exists {
					continue
				}
			}
			next[id] = kind
		}
		return rows.Err()
	}

	if err := loadIDs(`SELECT id FROM jobs_applied WHERE user_id = ?`, seenApplied, false); err != nil {
		return 0, err
	}
	if err := loadIDs(`SELECT id FROM jobs_skipped WHERE user_id = ?`, seenSkipped, true); err != nil {
		return 0, err
	}
	if err := loadIDs(`SELECT job_id FROM jobs_pending_review WHERE user_id = ?`, seenPendingReview, true); err != nil {
		return 0, err
	}
	if err := loadIDs(`SELECT job_id FROM jobs_approved_queue WHERE user_id = ?`, seenApproved, true); err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.byID = next
	c.mu.Unlock()
	return len(next), nil
}

func (c *jobSeenCache) reason(jobID string) string {
	if jobID == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	kind, ok := c.byID[jobID]
	if !ok {
		return ""
	}
	return kind.reason()
}

func (c *jobSeenCache) mark(jobID string, kind jobSeenKind) {
	if jobID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.byID[jobID]; ok {
		// Applied always wins; do not downgrade applied → skipped.
		if existing == seenApplied || kind == seenApplied {
			c.byID[jobID] = seenApplied
			return
		}
	}
	c.byID[jobID] = kind
}
