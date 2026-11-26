package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
)

func TestJobPageLinkedInNativeHookRejectsBusyDescriptionUntilComplete(t *testing.T) {
	database := pageHookDB(t)
	manager := pageHookManager(t, database)
	description := strings.Repeat("Develop reliable services, review code, and operate production systems. ", 8)
	browser := workerFixtureBrowser(t, func(raw string) string {
		return strings.Replace(pageHookHTML("linkedin", linkedInExpectedID(raw), description), "class='jobs-description-content__text'", "class='jobs-description-content__text' aria-busy='true'", 1)
	})
	bot := New(Config{UserID: pageTestUser, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore})
	job := linkedInJob{URL: "https://www.linkedin.com/jobs/view/12345678", Title: "Engineer", Company: "Acme"}
	page := navigateAndWait(t, browser, job.URL)
	result, err := bot.readJobPage(t.Context(), page, domain.PlatformLinkedIn, job)
	require.Equal(t, jobpage.FailureThin, jobpage.ErrorCode(err))
	require.Empty(t, result.Description)
	require.Zero(t, result.Metrics.HTTPAttempts)
	_, err = page.Eval("()=>document.querySelector('[aria-busy]').removeAttribute('aria-busy')")
	require.NoError(t, err)
	result, err = bot.readJobPage(t.Context(), page, domain.PlatformLinkedIn, job)
	require.NoError(t, err)
	require.Equal(t, jobpage.SourceDeterministic, result.Source)
	require.Contains(t, result.Description, "Develop reliable services")
	require.Zero(t, result.Metrics.HTTPAttempts)
	clicked, err := page.Eval("()=>sessionStorage.getItem('read-only-apply-click')==='true'")
	require.NoError(t, err)
	require.False(t, clicked.Value.Bool())
	var reads, failures int
	require.NoError(t, database.QueryRow("SELECT COUNT(*), SUM(CASE WHEN failure_code='description_incomplete' THEN 1 ELSE 0 END) FROM job_page_read_events WHERE user_id=?", pageTestUser).Scan(&reads, &failures))
	require.Equal(t, 2, reads)
	require.Equal(t, 1, failures)
}
