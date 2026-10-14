package resume

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestProfileLinks_NormalizeWebAddressesAndDOIs(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"example.com/portfolio", "https://example.com/portfolio"},
		{"//example.com/project", "https://example.com/project"},
		{" https://example.com/credential ", "https://example.com/credential"},
		{"javascript:alert(1)", ""}, {"data:text/html,test", ""}, {"file:///tmp/resume", ""},
		{"https://user:password@example.com", ""}, {"not a URL", ""}, {"https://", ""},
	} {
		t.Run(tc.input, func(t *testing.T) { require.Equal(t, tc.want, profileWebLink(tc.input)) })
	}
	require.Equal(t, "https://doi.org/10.1234/research", profileDOILink("doi:10.1234/research"))
	require.Equal(t, "https://doi.org/10.1234/research", profileDOILink("10.1234/research"))
	require.Empty(t, profileDOILink("javascript:alert(1)"))
	require.Equal(t, "mailto:sam@example.com", profileEmailLink("sam@example.com"))
}

func TestResumeHTML_UsesProfileLinksAndPreservesLegacyRendering(t *testing.T) {
	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Email: "sam@example.com", LinkedIn: "linkedin.com/in/sam", GitHub: "github.com/sam"},
		Projects:            []domain.Project{{Name: "Portfolio", Link: "example.com/work", Description: "Selected work"}},
		Certifications:      []domain.Certification{{Name: "Qualification", Link: "https://example.com/credential"}},
		Publications:        []domain.Publication{{Title: "Research", DOI: "10.1234/research"}},
	}
	html, err := renderResumeHTML(profile, "", DefaultSectionLabels())
	require.NoError(t, err)
	for _, target := range []string{"mailto:sam@example.com", "https://linkedin.com/in/sam", "https://github.com/sam", "https://example.com/work", "https://example.com/credential", "https://doi.org/10.1234/research"} {
		require.Contains(t, html, `href="`+target+`"`)
	}
	require.Contains(t, html, "View project:")
	require.Contains(t, html, "View credential:")
	require.Contains(t, html, "text-decoration: underline")
	legacy, err := renderResumeHTMLVersion(profile, "", DefaultSectionLabels(), LegacyPDFRendererVersion)
	require.NoError(t, err)
	require.Contains(t, legacy, `href="example.com/work"`, "old project link behaviour remains frozen")
	require.NotContains(t, legacy, "View project:")
	require.NotContains(t, legacy, "View credential:")
	require.NotContains(t, legacy, `href="https://example.com/credential"`)
	require.NotContains(t, legacy, `href="https://doi.org/10.1234/research"`)
}

func TestResumeHTML_UnsafeLinksRemainInactiveAndLinkOnlyProjectsHaveLabels(t *testing.T) {
	profile := &domain.ResumeProfile{
		Projects:       []domain.Project{{Name: "Unsafe project", Link: "javascript:alert(1)"}, {Link: "example.com/work"}},
		Certifications: []domain.Certification{{Name: "Unsafe credential", Link: "data:text/html,test"}},
	}
	html, err := renderResumeHTML(profile, "", DefaultSectionLabels())
	require.NoError(t, err)
	require.NotContains(t, html, "javascript:")
	require.NotContains(t, html, "data:text/html")
	require.Contains(t, html, "Unsafe project")
	require.Contains(t, html, `href="https://example.com/work">Project</a>`)
}

func TestTailorPrompt_PreservesSuppliedLinks(t *testing.T) {
	prompt, err := BuildTailorPrompt(&domain.ResumeProfile{Projects: []domain.Project{{Name: "Portfolio", Link: "https://example.com"}}}, "A relevant role")
	require.NoError(t, err)
	require.Contains(t, prompt, "Preserve supplied project, portfolio, certification, LinkedIn and GitHub URLs and publication DOIs exactly")
	require.Contains(t, prompt, "https://example.com")
}
