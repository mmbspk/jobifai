package bot_test

import (
	"os"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/user/jobifai/internal/bot"
)

func TestMain(m *testing.M) {
	// Migration failures are returned to each test. Suppress the repeated
	// successful-migration listing so regression failures remain readable.
	goose.SetLogger(goose.NopLogger())
	bot.RegisterTestAutomationRunner()
	os.Exit(m.Run())
}
