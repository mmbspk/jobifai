package auth

import (
	"errors"
	"fmt"
	"strings"
)

// ErrProtectedUser is returned when attempting to delete a reserved account.
var ErrProtectedUser = errors.New("cannot delete protected user account")

// userDataTables lists tables that store rows keyed by user_id (no FK cascade).
var userDataTables = []string{
	"settings",
	"secrets",
	"platform_sessions",
	"jobs_applied",
	"jobs_skipped",
	"jobs_pending_review",
	"jobs_approved_queue",
	"usage_totals",
}

// DeleteUser removes all data for userID and deletes the users row.
// Refresh tokens are removed via ON DELETE CASCADE when the user row is deleted.
func (s *UserStore) DeleteUser(userID string) error {
	if userID == "" || userID == "__default__" {
		return ErrProtectedUser
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range userDataTables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE user_id = ?", table), userID); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return tx.Commit()
}

// DeleteAllExcept keeps only the given user IDs and removes every other account.
func (s *UserStore) DeleteAllExcept(keepIDs ...string) (int, error) {
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		if id != "" {
			keep[id] = struct{}{}
		}
	}
	users, err := s.List()
	if err != nil {
		return 0, err
	}
	var deleted int
	for _, u := range users {
		if _, ok := keep[u.ID]; ok {
			continue
		}
		if err := s.DeleteUser(u.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// PruneE2ETestUsers deletes accounts created by Playwright (email *@e2e.test).
func (s *UserStore) PruneE2ETestUsers() (int, error) {
	rows, err := s.db.Query(`SELECT id FROM users WHERE email LIKE '%@e2e.test'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var n int
	for _, id := range ids {
		if err := s.DeleteUser(id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// IsE2ETestEmail reports whether email belongs to automated Playwright registration.
func IsE2ETestEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@e2e.test")
}

// RejectE2ERegistrationUnlessEnabled returns an error when e2e test emails register outside e2e mode.
func RejectE2ERegistrationUnlessEnabled(email string, e2eEnabled bool) error {
	if IsE2ETestEmail(email) && !e2eEnabled {
		return errors.New("registration not allowed for this email domain")
	}
	return nil
}
