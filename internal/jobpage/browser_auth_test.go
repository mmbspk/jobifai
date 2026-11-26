package jobpage

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBrowserAuthenticationRequiresTrustedOriginAndRealPath(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, test := range []struct {
		name, target, current string
		code                  FailureCode
	}{
		{"listing query is not authentication", "https://www.linkedin.com/jobs/view/123/", "https://www.linkedin.com/jobs/view/123/?next=/login", ""},
		{"LinkedIn checkpoint", "https://www.linkedin.com/jobs/view/123/", "https://www.linkedin.com/checkpoint/challenge", FailureAuth},
		{"SEEK trusted login broker", "https://www.seek.com.au/job/123", "https://login.seek.com/oauth/login", FailureAuth},
		{"SEEK sign-in", "https://www.seek.com.au/job/123", "https://www.seek.com.au/sign-in", FailureAuth},
		{"foreign login path", "https://www.linkedin.com/jobs/view/123/", "https://attacker.invalid/login", FailureDrift},
		{"SEEK login cannot authenticate LinkedIn", "https://www.linkedin.com/jobs/view/123/", "https://login.seek.com/oauth/login", FailureDrift},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, _, err := NormalizeTarget(Target{URL: test.target, Title: "Engineer", Company: "Acme"})
			require.NoError(t, err)
			reader, page := readerFixture(t, browser, target, fixtureHTML("<section data-testid='job-description'>"+fixtureText()+"</section>"))
			require.NoError(t, page.Navigate(test.current))
			require.NoError(t, page.Timeout(5*time.Second).WaitLoad())
			result, err := reader.Known(t.Context(), target, DefaultLimits())
			require.Equal(t, test.code, ErrorCode(err))
			if test.code == "" {
				require.NoError(t, err)
				require.Contains(t, result.Description, "Build secure services")
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBrowserInlineLoginCannotBorrowMissingListingIdentity(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target := Target{URL: "https://www.seek.com.au/job/123"}
	reader, _ := readerFixture(t, browser, target, "<form action='/login'><input type='password'></form>")
	snapshot, err := reader.Capture(t.Context(), target, DefaultLimits())
	require.Equal(t, FailureAuth, ErrorCode(err))
	require.Empty(t, snapshot.Nodes)
	require.Empty(t, snapshot.Candidates)
}
