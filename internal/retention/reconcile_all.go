package retention

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// ScheduleReconcileAllUsers enqueues backlog cleanup for every user with applied jobs.
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
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				continue
			}
			if _, err := s.ReconcileUser(ctx, userID); err != nil {
				log.Warn().Err(err).Str("user_id", userID).Msg("retention: user reconcile failed")
			}
		}
	}()
}
