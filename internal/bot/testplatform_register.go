package bot

import (
	"context"
	"sync"
)

var registerTestRunnerOnce sync.Once

// RegisterTestAutomationRunner installs the no-browser test_automation platform runner.
// Call from tests before starting the bot with PlatformTestAutomation.
func RegisterTestAutomationRunner() {
	registerTestRunnerOnce.Do(func() {
		registerRunner(PlatformTestAutomation, platformRunnerFunc(runTestAutomation))
	})
}

func runTestAutomation(ctx context.Context, b *Bot) {
	b.warmSeenCache()

	job := linkedInJob{
		ID:       "test-automation-job-1",
		Company:  "Mock Job Board",
		Title:    "Operations Associate",
		Location: "Remote",
		URL:      "https://example.com/jobs/test-automation-1",
	}

	score := 8
	reasoning := "mock scorer not configured"
	if b.cfg.Scorer != nil {
		if result, err := b.cfg.Scorer.EvaluateJob(ctx, b.cfg.Profile, "Synthetic job description for integration test."); err == nil {
			score = result.Score
			reasoning = result.Reasoning
		}
	}

	select {
	case <-ctx.Done():
		return
	case <-b.stopCh:
		return
	default:
	}

	b.recordSkipped(job, "test_automation_complete", score, reasoning, nil)
}
