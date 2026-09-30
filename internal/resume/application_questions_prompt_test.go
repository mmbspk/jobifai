package resume

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestBuildApplicationQuestionsPrompt_IncludesApplicationDefaults(t *testing.T) {
	t.Parallel()
	prof := domain.ResumeProfile{
		ApplicationDefaults: domain.ApplicationDefaults{
			NoticePeriod: "8 weeks", SalaryExpectation: "88000", RequiresSponsorship: true,
		},
		ExperienceDetails: []domain.ExperienceDetail{{
			Position: "Registered Nurse", EmploymentPeriod: "2019 – Present",
		}},
	}
	_, user, err := BuildApplicationQuestionsPrompt(&prof, "Hospital", []string{"Notice period?"})
	require.NoError(t, err)
	low := strings.ToLower(user)
	require.Contains(t, low, "8 weeks")
	require.Contains(t, low, "88000")
	require.Contains(t, low, "requires employer sponsorship: yes")
}

func TestBuildApplicationQuestionsPrompt_SponsorshipFalseExplicit(t *testing.T) {
	t.Parallel()
	prof := domain.ResumeProfile{
		ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: false},
	}
	_, user, err := BuildApplicationQuestionsPrompt(&prof, "Hospital", []string{"Sponsorship?"})
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(user), "requires employer sponsorship: no")
}
