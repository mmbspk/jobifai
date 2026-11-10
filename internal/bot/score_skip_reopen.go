package bot

import (
	"database/sql"
	"regexp"
	"strings"
)

var scoreBelowThresholdSkipRE = regexp.MustCompile(`^score (\d+) < (\d+)$`)

// effectiveMinSuitabilityScore returns the configured minimum, defaulting to 7 when unset.
func (b *Bot) effectiveMinSuitabilityScore() int {
	min := b.cfg.Settings.JobSuitabilityScore
	if min == 0 {
		min = 7
	}
	return min
}

// parseScoreBelowThresholdSkip returns the job score and former threshold from a score skip reason.
func parseScoreBelowThresholdSkip(reason string) (jobScore, formerMin int, ok bool) {
	m := scoreBelowThresholdSkipRE.FindStringSubmatch(strings.TrimSpace(reason))
	if len(m) != 3 {
		return 0, 0, false
	}
	jobScore = atoiJobScore(m[1])
	formerMin = atoiJobScore(m[2])
	return jobScore, formerMin, true
}

func atoiJobScore(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// reopenScoreSkippedIfEligible removes a jobs_skipped row when it was skipped only for being
// below a former suitability threshold and the stored score now meets the current threshold.
func (b *Bot) reopenScoreSkippedIfEligible(jobID string) bool {
	if jobID == "" || b.cfg.DB == nil {
		return false
	}
	var reason string
	var storedScore sql.NullInt64
	err := b.cfg.DB.QueryRow(
		`SELECT skip_reason, suitability_score FROM jobs_skipped WHERE id = ? AND user_id = ?`,
		jobID, b.cfg.UserID,
	).Scan(&reason, &storedScore)
	if err != nil {
		return false
	}
	parsedScore, _, ok := parseScoreBelowThresholdSkip(reason)
	if !ok {
		return false
	}
	jobScore := parsedScore
	if storedScore.Valid && int(storedScore.Int64) > 0 {
		jobScore = int(storedScore.Int64)
	}
	minScore := b.effectiveMinSuitabilityScore()
	if jobScore < minScore {
		return false
	}
	res, err := b.cfg.DB.Exec(
		`DELETE FROM jobs_skipped WHERE id = ? AND user_id = ? AND skip_reason = ?`,
		jobID, b.cfg.UserID, reason,
	)
	if err != nil {
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	if b.seenCache != nil {
		b.seenCache.forget(jobID)
	}
	b.log.Info().
		Str("job_id", jobID).
		Int("score", jobScore).
		Int("min_score", minScore).
		Str("former_skip_reason", reason).
		Msg("reopened score-threshold skip after suitability threshold change")
	return true
}
