package auth_test

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"github.com/user/jobifai/internal/auth"
)

func TestMain(m *testing.M) {
	auth.SetBcryptCostForTest(bcrypt.MinCost)
	os.Exit(m.Run())
}
