package bot

import (
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"testing"
)

func TestProfileBinding_ExpandedScalarFactsUseCurrentValues(t *testing.T) {
	profile := &domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{Availability: "Weekdays", EarliestStartDate: "2027-01-04", AdditionalDetails: "Occasional travel", Relocation: &domain.ApplicationRelocation{Details: "Local roles preferred"}}}
	for key, value := range map[string]string{
		"application_defaults.availability":        "Weekdays",
		"application_defaults.earliest_start_date": "2027-01-04",
		"application_defaults.additional_details":  "Occasional travel",
		"application_defaults.relocation.details":  "Local roles preferred",
	} {
		require.True(t, knownProfileBinding(key))
		actual, ok := resolveProfileBinding(key, profile)
		require.True(t, ok)
		require.Equal(t, value, actual)
	}
	profile.ApplicationDefaults.Availability = "Evenings"
	actual, ok := resolveProfileBinding("application_defaults.availability", profile)
	require.True(t, ok)
	require.Equal(t, "Evenings", actual)
}

func TestProfileBinding_NullableRelocationAndSponsorshipStayIndependent(t *testing.T) {
	no := false
	yes := true
	profile := &domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: &no, WorkRights: []domain.ApplicationWorkRight{{Country: "Australia", RequiresSponsorship: &no}, {Country: "New Zealand", RequiresSponsorship: &yes}}}}
	for _, relocation := range []*domain.ApplicationRelocation{nil, {}, {Willing: &no}, {Willing: &yes}} {
		profile.ApplicationDefaults.Relocation = relocation
		value, ok := resolveProfileBinding("application_defaults.relocation.willing", profile)
		if relocation == nil || relocation.Willing == nil {
			require.False(t, ok)
			require.Empty(t, value)
		} else {
			require.True(t, ok)
			if *relocation.Willing {
				require.Equal(t, "Yes", value)
			} else {
				require.Equal(t, "No", value)
			}
		}
	}
	require.False(t, knownProfileBinding("application_defaults.work_rights.0.requires_sponsorship"))
	profile.ApplicationDefaults.RequiresSponsorship = nil
	value, ok := resolveProfileBinding("application_defaults.requires_sponsorship", profile)
	require.False(t, ok)
	require.Empty(t, value)
	require.Equal(t, []domain.ApplicationWorkRight{{Country: "Australia", RequiresSponsorship: &no}, {Country: "New Zealand", RequiresSponsorship: &yes}}, profile.ApplicationDefaults.WorkRights)
}
