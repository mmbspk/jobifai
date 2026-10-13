package resume

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestAustralia_LanguagesHaveOneSection(t *testing.T) {
	market := LoadMarketByName("../../resume_markets", "Australia")
	require.NotNil(t, market)
	for _, prompt := range []string{market.ResumePrompt, market.TailoredPrompt} {
		require.Contains(t, prompt, `spoken languages only in the structured "languages" field, never in "skills"`)
		require.NotContains(t, prompt, "include them as the last category")
	}
	profile := &domain.ResumeProfile{
		Skills:    []string{"Languages & Frameworks: Go, React"},
		Languages: []domain.Language{{Language: "English", Proficiency: "Fluent"}},
	}
	html, err := renderResumeHTML(profile, "", market.LabelsOrDefault())
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(html, "English"))
	require.Equal(t, 1, strings.Count(html, `id="skills-languages"`))
	// This extraction depends on resumeTempl's skills-technical ID and closing section tag.
	// Update it if the template changes its section structure.
	require.Contains(t, html, `id="skills-technical"`)
	skills := strings.Split(strings.Split(html, `id="skills-technical"`)[1], "</section>")[0]
	require.NotContains(t, skills, "English")
	require.Contains(t, skills, "Go, React", "programming languages still belong in skills")
}
