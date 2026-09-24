package handler_test

import (
	"os"
	"testing"

	"github.com/user/jobifai/internal/bot"
)

func TestMain(m *testing.M) {
	bot.RegisterTestAutomationRunner()
	os.Exit(m.Run())
}
