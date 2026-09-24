package bot

import "github.com/user/jobifai/internal/domain"

// PlatformTestAutomation is a synthetic job-board runner registered during tests
// (see test_automation_runner_test.go). It exercises the bot loop without Rod.
const PlatformTestAutomation domain.Platform = "test_automation"
