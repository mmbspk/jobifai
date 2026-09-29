package dataset_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/resume"
)

// scenarioEvidence lists substrings that must appear in the production scoring prompt
// for the label to be justified from ForScoring-visible facts.
var scenarioEvidence = map[string][]string{
	"strong_exact_fit":           {"acute care", "Registered Nurse", "Bachelor", "Nursing"},
	"transferable_fit":           {"Enrolled Nurse", "enrolled nurse", "aged care"},
	"borderline_fit":             {"Healthcare Assistant", "vitals"},
	"seniority_mismatch":         {"Graduate Nurse", "8 years"},
	"mandatory_skill_mismatch":   {"med-surg", "ICU", "ventilation"},
	"qualification_mismatch":     {"Enrolled Nursing", "Bachelor"},
	"location_mismatch":          {"Melbourne", "Perth"},
	"insufficient_experience":    {"Student Nurse", "5 years"},
	"overqualified":              {"Director of Nursing", "entry-level"},
	"title_diff_skills_fit":      {"Clinical Care Coordinator", "rostering"},
	"domain_mismatch":            {"pediatric oncology", "paralegal"},
	"tech_mismatch":              {"Agile", "SAP"},
	"strong_exact_fit_pm":        {"Construction Project Manager", "budget control"},
	"licence_mismatch":           {"Maintenance Technician", "Licensed electrician"},
	"education_level_mismatch":   {"Certificate", "Master"},
}

func TestJobScoringScenarios_VisibleEvidenceInProductionPrompt(t *testing.T) {
	t.Parallel()
	b, err := dataset.Load(dataset.LoadRequest{Task: domain.TaskJobScoring, Version: "full", Source: dataset.SourceSynthetic})
	require.NoError(t, err)
	seenTags := map[string]bool{}
	for _, c := range b.Cases {
		var exp struct {
			Scenario string `json:"scenario"`
		}
		require.NoError(t, json.Unmarshal(c.Expect, &exp))
		if exp.Scenario == "" && len(c.Tags) > 0 {
			exp.Scenario = c.Tags[0]
		}
		seenTags[exp.Scenario] = true
		needles, ok := scenarioEvidence[exp.Scenario]
		require.True(t, ok, "missing evidence table for scenario %q case %s", exp.Scenario, c.ID)

		var in struct {
			Profile        json.RawMessage `json:"profile"`
			JobDescription string          `json:"job_description"`
		}
		require.NoError(t, json.Unmarshal(c.Input, &in))
		var prof domain.ResumeProfile
		require.NoError(t, json.Unmarshal(in.Profile, &prof))
		prompt, err := resume.BuildJobScoringPrompt(&prof, in.JobDescription)
		require.NoError(t, err)
		low := strings.ToLower(prompt)
		found := 0
		for _, n := range needles {
			if strings.Contains(low, strings.ToLower(n)) {
				found++
			}
		}
		require.Greater(t, found, 0, "scenario %s case %s: none of %v found in scoring prompt", exp.Scenario, c.ID, needles)
	}
	for tag := range scenarioEvidence {
		require.True(t, seenTags[tag], "dataset should include scenario tag %q", tag)
	}
}
