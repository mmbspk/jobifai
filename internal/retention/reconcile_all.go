package retention

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// ScheduleReconcileAllUsers enqueues backlog cleanup for every user with applied jobs.
// User IDs are collected into a slice and the cursor is closed before any
// per-user reconcile work begins, so nested queries inside ReconcileUser do not
// compete with the outer cursor for the bounded connection pool.
func (s *Service) ScheduleReconcileAllUsers() {
	if s == nil || s.DB == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM jobs_applied`)
		if err != nil {
			log.Warn().Err(err).Msg("retention: reconcile all users query failed")
			return
		}
		var userIDs []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				log.Warn().Err(err).Msg("retention: scan user_id failed")
				continue
			}
			userIDs = append(userIDs, userID)
		}
		if err := rows.Err(); err != nil {
			log.Warn().Err(err).Msg("retention: reconcile all users iteration error")
		}
		_ = rows.Close()

		for _, userID := range userIDs {
			if _, err := s.ReconcileUser(ctx, userID); err != nil {
				log.Warn().Err(err).Str("user_id", userID).Msg("retention: user reconcile failed")
			}
		}
	}()
}
