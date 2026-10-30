package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestEvaluationFailuresContinueUntilThreeDistinctJobs(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformSeek, domain.PlatformLinkedIn} {
		for _, stage := range []string{"score", "halal"} {
			t.Run(string(platform)+"/"+stage, func(t *testing.T) {
				db := testDB(t)
				b := New(Config{DB: db, UserID: "u1", Scorer: stubScorer{result: domain.JobScore{Score: 9}}})
				if stage == "score" {
					b.cfg.Scorer = stubScorer{err: context.DeadlineExceeded}
				} else {
					b.cfg.HalalChecker = stubHalal{err: context.DeadlineExceeded}
				}
				for i, id := range []string{"1", "1", "2", "3"} {
					var ok bool
					if platform == domain.PlatformSeek {
						_, _, _, ok = b.checkSeekScore(context.Background(), seekJob{ID: id, Title: "Role"}, "description")
					} else {
						_, _, _, ok = b.checkScore(context.Background(), linkedInJob{ID: id, Title: "Role"}, "description")
					}
					require.False(t, ok)
					require.Equal(t, i == 3, b.stopped(context.Background()))
				}
				require.Equal(t, domain.BotStateError, b.State())
				require.Equal(t, "3 consecutive jobs failed AI evaluation", b.stopReason(context.Background()))
				var count int
				require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM jobs_skipped WHERE failure_code='evaluation_failed'").Scan(&count))
				require.Equal(t, 3, count)
				require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM jobs_applied").Scan(&count))
				require.Zero(t, count)
			})
		}
	}
}

func TestEvaluationSuccessResetsFailureStreakIncludingLowScore(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformSeek, domain.PlatformLinkedIn} {
		t.Run(string(platform), func(t *testing.T) {
			b := New(Config{Scorer: stubScorer{err: context.DeadlineExceeded}})
			evaluate := func(id string) {
				if platform == domain.PlatformSeek {
					b.checkSeekScore(context.Background(), seekJob{ID: id}, "description")
				} else {
					b.checkScore(context.Background(), linkedInJob{ID: id}, "description")
				}
			}
			evaluate("1")
			evaluate("2")
			b.cfg.Scorer = stubScorer{result: domain.JobScore{Score: 1}}
			evaluate("success-low-score")
			require.Empty(t, b.evaluationFailedJobs)
			b.cfg.Scorer = stubScorer{err: context.DeadlineExceeded}
			for i := 0; i < 2; i++ {
				evaluate(fmt.Sprint(i + 3))
			}
			require.False(t, b.stopped(context.Background()))
		})
	}
}

func TestCancelledEvaluationDoesNotCountAsServiceFailure(t *testing.T) {
	b := New(Config{Scorer: stubScorer{err: context.Canceled}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.checkSeekScore(ctx, seekJob{ID: "cancelled"}, "description")
	require.Empty(t, b.evaluationFailedJobs)
	require.Empty(t, b.automaticStopReason)
}
