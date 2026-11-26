package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeOwnedDescriptionFixture(extra string) string {
	return `<main data-job-id="123"><h1>Engineer</h1><a aria-label="Company, Acme.">Acme</a><button onclick="window.applied=true">Easy Apply</button><section class="jobs-description-content__text"><p>` + strings.Repeat("Current owned requirements and responsibilities. ", 8) + `</p><section><h2>About us</h2><p>Acme hires Engineer candidates for the current team.</p></section>` + extra + `</section></main>`
}

func TestLinkedInNativeDescriptionFiltersHiddenUnrelatedAndForeignText(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return nativeOwnedDescriptionFixture(`<p hidden>HIDDEN PRIVATE ANSWER</p><p aria-hidden="true">ACCESSIBILITY HIDDEN ANSWER</p><p style="display:none">CSS HIDDEN ANSWER</p><aside>UNRELATED RECOMMENDATION</aside><nav>UNRELATED NAVIGATION</nav><form><input value="PRIVATE"><p>PRIVATE FORM ANSWER</p></form><section data-job-id="999"><p>FOREIGN VACANCY TEXT</p></section><section id="JobDetails_AboutTheJob_999"><p>FOREIGN SDUI DESCRIPTION</p></section><div style="display:contents">Owned display contents text.<p>Owned display contents child.</p></div>`)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
	result, err := evalLinkedInJob(page, "123", false, false, linkedInJob{Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.False(t, result.DescriptionIncomplete)
	require.Contains(t, result.Description, "Current owned requirements")
	require.Contains(t, result.Description, "About us")
	require.Contains(t, result.Description, "Acme hires Engineer candidates")
	require.Contains(t, result.Description, "Owned display contents text.")
	require.Contains(t, result.Description, "Owned display contents child.")
	for _, excluded := range []string{"HIDDEN", "UNRELATED", "PRIVATE", "FOREIGN"} {
		require.NotContains(t, result.Description, excluded)
	}
}

func TestLinkedInNativeForeignDescriptionControlsCannotReceiveInputOrBlockReading(t *testing.T) {
	for _, test := range []struct {
		name  string
		extra string
	}{
		{"foreign subject", `<section data-job-id="999">`},
		{"foreign SDUI subject", `<section id="JobDetails_AboutTheJob_999">`},
		{"same numeric ID SEEK issuer", `<section data-job-id="123"><h2><a href="https://www.seek.com.au/job/123">Foreign SEEK vacancy</a></h2>`},
		{"different LinkedIn subject", `<section><h2><a href="https://www.linkedin.com/jobs/view/999/">Foreign LinkedIn vacancy</a></h2>`},
		{"hidden subject", `<section hidden>`},
		{"unrelated panel", `<aside>`},
		{"unrelated complementary panel", `<div role="complementary"><h2><a href="https://www.seek.com.au/job/123">UNRELATED SEEK vacancy</a></h2>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			end := "</section>"
			switch test.name {
			case "unrelated panel":
				end = "</aside>"
			case "unrelated complementary panel":
				end = "</div>"
			}
			br := workerFixtureBrowser(t, func(string) string {
				return nativeOwnedDescriptionFixture(test.extra + `<button data-testid="expandable-text-button" aria-hidden="true" onclick="window.foreignClicked=true">Show more</button><div aria-busy="true" style="height:10px;overflow:hidden"><p>FOREIGN OR HIDDEN PANEL TEXT</p></div>` + end)
			})
			page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
			result, err := evalLinkedInJob(page, "123", false, true, linkedInJob{Title: "Engineer", Company: "Acme"})
			require.NoError(t, err)
			require.True(t, result.IdentityVerified)
			require.False(t, result.DescriptionExpanded)
			require.False(t, result.DescriptionExpandable)
			require.False(t, result.DescriptionIncomplete, "foreign clipping/loading cannot block the owned description")
			require.Contains(t, result.Description, "Current owned requirements")
			require.NotContains(t, result.Description, "FOREIGN")
			state, err := page.Eval(`()=>({foreign:window.foreignClicked===true,applied:window.applied===true})`)
			require.NoError(t, err)
			require.False(t, state.Value.Get("foreign").Bool())
			require.False(t, state.Value.Get("applied").Bool())
		})
	}
}

func TestLinkedInNativeDescriptionPreservesBreaksAndTinyPunctuation(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return nativeOwnedDescriptionFixture(`<p>first<br>second<span style="display:inline-block;width:1px;height:1px">!</span></p>`)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
	result, err := evalLinkedInJob(page, "123", false, false, linkedInJob{Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	require.False(t, result.DescriptionIncomplete)
	require.Contains(t, result.Description, "first\nsecond!")
}

func TestLinkedInNativeZeroHeightClippedDescriptionCannotComplete(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return strings.Replace(nativeOwnedDescriptionFixture(""), `class="jobs-description-content__text"`, `class="jobs-description-content__text" style="height:0;overflow:hidden"`, 1)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
	result, err := evalLinkedInJob(page, "123", false, false, linkedInJob{Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.True(t, result.DescriptionIncomplete)
	require.False(t, result.DescriptionExpanded)
}

func TestLinkedInNativeDescriptionCopyCannotReplaceHeaderMetadata(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return nativeOwnedDescriptionFixture(`<h1>Different described team title</h1><a aria-label="Company, Other team.">Other team description copy.</a><section data-job-id="123"><p>Current owned description component.</p></section>`)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
	result, err := evalLinkedInJob(page, "123", false, false, linkedInJob{})
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.Equal(t, "Engineer", result.Title)
	require.Equal(t, "Acme", result.Company)
	require.Contains(t, result.Description, "Different described team title")
	require.Contains(t, result.Description, "Other team description copy.")
	require.Contains(t, result.Description, "Current owned description component.")
}

func TestLinkedInNativeLateClippedDescriptionCannotComplete(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return nativeOwnedDescriptionFixture(strings.Repeat("<span>Owned marker.</span>", 520) + `<div style="height:0;overflow:clip"><p>Clipped description tail.</p></div>`)
	})
	page := navigateAndWait(t, br, "https://www.linkedin.com/jobs/view/123/")
	result, err := evalLinkedInJob(page, "123", false, false, linkedInJob{Title: "Engineer", Company: "Acme"})
	require.NoError(t, err)
	require.True(t, result.IdentityVerified)
	require.True(t, result.DescriptionIncomplete, "every extracted descendant must be checked for clipping")
}
