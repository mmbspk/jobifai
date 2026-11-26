package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
)

func TestJobPageLinkedInNativeCompletenessPreservesFailureAndHiddenTemplate(t *testing.T) {
	for _, test := range []struct {
		name, extra string
		incomplete  bool
	}{
		{"ignored disclosure", `<button data-testid="expandable-text-button" aria-hidden="true">… more</button>`, true},
		{"hidden duplicate template", `</section><section data-testid="job-description" hidden>Loading...`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := pageHookDB(t)
			manager := pageHookManager(t, database)
			description := strings.Repeat("Current complete requirements for reliable engineer services. ", 8)
			br := workerFixtureBrowser(t, func(string) string {
				return `<main data-job-id="12345678"><h1>Engineer</h1><a aria-label="Company, Acme.">Acme</a><button onclick="window.unexpectedApply=true">Easy Apply</button><section class="jobs-description-content__text"><p>` + description + `</p>` + test.extra + `</section></main>`
			})
			page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/12345678/")
			bot := New(Config{UserID: pageTestUser, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore})
			result, err := bot.readJobPage(t.Context(), page, domain.PlatformLinkedIn, linkedInJob{URL: "https://www.linkedin.com/jobs/view/12345678/", Title: "Engineer", Company: "Acme"})
			if test.incomplete {
				require.Equal(t, jobpage.FailureThin, jobpage.ErrorCode(err))
				require.Empty(t, result.Description)
			} else {
				require.NoError(t, err)
				require.Contains(t, result.Description, "Current complete requirements")
				require.NotContains(t, result.Description, "Loading...")
				require.Equal(t, jobpage.SourceDeterministic, result.Source)
			}
			require.Zero(t, result.Metrics.HTTPAttempts)
			state, err := page.Eval(`()=>window.unexpectedApply===true`)
			require.NoError(t, err)
			require.False(t, state.Value.Bool())
		})
	}
}
