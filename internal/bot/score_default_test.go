package bot

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"testing"
)

func TestDefaultSuitabilityThresholdIsSeven(t *testing.T) {
	for _, platform := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
		for _, configured := range []int{0, 6} {
			t.Run(string(platform)+string(rune('0'+configured)), func(t *testing.T) {
				db := testDB(t)
				b := New(Config{DB: db, UserID: "u1", Settings: domain.GeneralSettings{JobSuitabilityScore: configured}, Scorer: stubScorer{result: domain.JobScore{Score: 6}}})
				var ok bool
				if platform == domain.PlatformSeek {
					_, _, _, ok = b.checkSeekScore(context.Background(), seekJob{ID: "score-test"}, "description")
				} else {
					_, _, _, ok = b.checkScore(context.Background(), linkedInJob{ID: "score-test"}, "description")
				}
				if configured == 6 {
					require.True(t, ok)
					return
				}
				require.False(t, ok)
				var reason string
				require.NoError(t, db.QueryRow("SELECT skip_reason FROM jobs_skipped WHERE id='score-test'").Scan(&reason))
				require.Equal(t, "score 6 < 7", reason)
			})
		}
	}
}
