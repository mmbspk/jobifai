package bot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

const seekPermanentResidentChoice = "I'm a permanent resident and/or NZ citizen"

func seekWorkRightsField() SnapField {
	return SnapField{Ref: "f0", Label: "Which of the following statements best describes your right to work in Australia?", Type: SnapTypeSelect, Required: true, Options: []string{
		"I'm an Australian citizen", seekPermanentResidentChoice, "I have a family/partner visa with no restrictions",
		"I have a graduate temporary work visa", "I have a holiday temporary work visa",
		"I have a temporary visa with restrictions on work location (e.g. skilled regional visa 491)",
		"I have a temporary protection or safe haven enterprise work visa",
		"I have a temporary visa with no restrictions (e.g. doctoral student)",
		"I have a temporary visa with restrictions on work hours (e.g. student visa, retirement visa)",
		"I have a temporary visa with restrictions on industry (e.g. temporary activity visa 408)",
		"I require sponsorship to work for a new employer (e.g. 482, 457)",
	}}
}

func seekLegacyPRProfile() *domain.ResumeProfile {
	profile := groundedFixtureProfile()
	profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "Germany", Status: "yes"}, {Country: "Australia", Status: "yes", VisaType: "PR", WorkRestrictions: "None"}}
	return profile
}

func seekLegacyPREvidence() []ProfileEvidence {
	return []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: "yes"}, {Path: "/application_defaults/work_rights/1/visa_type", Quote: "PR"}}
}

func TestSeekWorkRights_LegacyPRGroupedChoiceNeedsIndependentVerification(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "approved", false: "rejected"}[approved], func(t *testing.T) {
			profile := seekLegacyPRProfile()
			field := seekWorkRightsField()
			snap := makeSnap(field)
			snap.Platform = "seek"
			proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: seekPermanentResidentChoice, Evidence: seekLegacyPREvidence()}
			raw, err := json.Marshal([]LLMFieldProposal{proposal})
			require.NoError(t, err)
			server := newCountingServer(t, func(body []byte) string {
				if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					require.Contains(t, string(body), "/application_defaults/work_rights/1/visa_type")
					return fixtureGroundedVerdicts(body, approved)
				}
				return string(raw)
			})
			proposals, err := NewLLMFormInterpreter(server.newClient(t)).InterpretFields(t.Context(), profile, snap, []string{"f0"})
			require.NoError(t, err)
			results := validateLLMProposals(proposals, []string{"f0"}, snap, profile)
			require.Len(t, results, 1)
			require.Equal(t, !approved, results[0].Pending)
			if approved {
				require.Equal(t, seekPermanentResidentChoice, results[0].Value)
				require.Equal(t, ProvenanceVerifiedByModel, results[0].Provenance)
			}
			require.Equal(t, int32(2), server.requests.Load(), "the alias never substitutes for independent meaning verification")
		})
	}
}

func TestSeekWorkRights_LegacyPRAuthorityAndChoiceGuards(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*domain.ResumeProfile, *SnapField, *LLMFieldProposal)
	}{
		{"plain yes is not residency", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].VisaType = ""
			q.Evidence = q.Evidence[:1]
		}},
		{"citizenship only", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			q.Value = "I'm an Australian citizen"
		}},
		{"AND citizenship", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			q.Value = "I'm a permanent resident and NZ citizen"
			f.Options = append(f.Options, q.Value)
		}},
		{"different visa class", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			q.Value = "I have a family/partner visa with no restrictions"
		}},
		{"pending residency", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = "pending"
			q.Evidence[0].Quote = "pending"
		}},
		{"negative authority", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = "no"
			q.Evidence[0].Quote = "no"
		}},
		{"citizen status cannot reuse stale PR", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = "citizen"
			q.Evidence[0].Quote = "citizen"
		}},
		{"clipped PR cannot hide pending application", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].VisaType = "PR application pending"
		}},
		{"clipped status cannot hide negation", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = "yes but not currently authorised"
		}},
		{"generic citizenship quote cannot hide NZ subtype", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = "New Zealand citizen"
			p.ApplicationDefaults.WorkRights[1].VisaType = ""
			q.Evidence = []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: "citizen"}}
		}},
		{"third party prose cannot grant grouped status", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.Summary = "I processed permanent resident applications for clients"
			q.Evidence = []ProfileEvidence{{Path: "/summary", Quote: p.Summary}}
		}},
		{"past residency cannot grant grouped status", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.AdditionalDetails = "I held permanent resident status in 2011"
			q.Evidence = []ProfileEvidence{{Path: "/application_defaults/additional_details", Quote: p.ApplicationDefaults.AdditionalDetails}}
		}},
		{"missing authority", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].Status = ""
			q.Evidence = q.Evidence[1:]
		}},
		{"PR application is not current PR", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].VisaType = "permanent resident application pending"
			q.Evidence[1].Quote = p.ApplicationDefaults.WorkRights[1].VisaType
		}},
		{"other country record", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[0].VisaType = "PR"
			q.Evidence = []ProfileEvidence{{Path: "/application_defaults/work_rights/0/status", Quote: "yes"}, {Path: "/application_defaults/work_rights/0/visa_type", Quote: "PR"}}
		}},
		{"NZ question", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.Label = "Which statement describes your right to work in New Zealand?"
		}},
		{"ambiguous jurisdiction", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.HelpText = "For Australia and New Zealand"
		}},
		{"missing jurisdiction", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.Label = "Which statement describes your right to work?"
		}},
		{"expiry is retained", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			p.ApplicationDefaults.WorkRights[1].VisaExpiryDate = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		}},
		{"free text does not make a grouped claim", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.Type = SnapTypeTextarea
			f.Options = nil
		}},
		{"unobserved choice", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.Options = []string{"I'm an Australian citizen"}
		}},
		{"duplicate choice", func(p *domain.ResumeProfile, f *SnapField, q *LLMFieldProposal) {
			f.Options = append(f.Options, seekPermanentResidentChoice)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := seekLegacyPRProfile()
			field := seekWorkRightsField()
			proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: seekPermanentResidentChoice, Evidence: seekLegacyPREvidence()}
			test.mutate(profile, &field, &proposal)
			_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
			require.False(t, compatible, "unsupported status/category must never reach an approving verifier")
		})
	}
}

func TestSeekWorkRights_PlainStatusAnswersOnlyAuthorization(t *testing.T) {
	profile := seekLegacyPRProfile()
	profile.ApplicationDefaults.WorkRights[1].VisaType = ""
	field := SnapField{Ref: "f0", Label: "Are you legally authorised to work in Australia?", Type: SnapTypeRadio, Options: []string{"Yes", "No"}, Required: true}
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "Yes", Evidence: seekLegacyPREvidence()[:1]}
	_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.True(t, compatible)
	field.Label = "Are you an Australian citizen?"
	require.False(t, supportedWorkRightAlternativeChoice(field, seekPermanentResidentChoice, proposal.Evidence))
}

func TestSeekWorkRights_CanonicalResidencyCodeSupportsGroupedChoice(t *testing.T) {
	profile := seekLegacyPRProfile()
	profile.ApplicationDefaults.WorkRights[1].Status = "permanent_residency"
	profile.ApplicationDefaults.WorkRights[1].VisaType = ""
	field := seekWorkRightsField()
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: seekPermanentResidentChoice, Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: "permanent_residency"}}}
	_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.True(t, compatible)
}

func TestSeekWorkRights_CountryQualifierDoesNotEraseSameResidencyFact(t *testing.T) {
	profile := seekLegacyPRProfile()
	profile.ApplicationDefaults.WorkRights[1].Status = "Australian permanent resident"
	profile.ApplicationDefaults.WorkRights[1].VisaType = ""
	field := seekWorkRightsField()
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: seekPermanentResidentChoice, Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: "permanent resident"}}}
	_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.True(t, compatible)
}

func TestSeekWorkRights_ExplicitNZCitizenBranchIsNotAUcitizenship(t *testing.T) {
	profile := seekLegacyPRProfile()
	profile.ApplicationDefaults.WorkRights[1].Status = "New Zealand citizen"
	profile.ApplicationDefaults.WorkRights[1].VisaType = ""
	field := seekWorkRightsField()
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: seekPermanentResidentChoice, Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: "New Zealand citizen"}}}
	_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.True(t, compatible, "the captured alternative permits its explicit NZ branch")
	proposal.Value = "I'm an Australian citizen"
	// Candidate acceptance alone grants no authority: without an independent
	// grounded proof the different citizenship declaration stays pending.
	results := validateLLMProposals([]LLMFieldProposal{proposal}, []string{"f0"}, makeSnap(field), profile)
	require.True(t, results[0].Pending)
	proposal.Value = "I'm a permanent resident and NZ citizen"
	field.Options = append(field.Options, proposal.Value)
	_, compatible = prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.False(t, compatible, "a sole NZ-citizen fact cannot become an AND residency claim")
}

func TestSeekWorkRights_CitizenOrResidentPreservesEitherNamedAUStatus(t *testing.T) {
	for _, status := range []string{"citizen", "permanent_residency"} {
		t.Run(status, func(t *testing.T) {
			profile := seekLegacyPRProfile()
			profile.ApplicationDefaults.WorkRights[1].Status = status
			profile.ApplicationDefaults.WorkRights[1].VisaType = ""
			field := seekWorkRightsField()
			field.Options = []string{"I'm an Australian citizen or permanent resident", "I require sponsorship"}
			proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: field.Options[0], Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/1/status", Quote: status}}}
			_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
			require.True(t, compatible, "one explicit AU status supports the inclusive category")
			results := validateLLMProposals([]LLMFieldProposal{proposal}, []string{"f0"}, makeSnap(field), profile)
			require.True(t, results[0].Pending, "category recognition alone never grants a verified answer")
		})
	}
}

func TestSeekWorkRights_ExplicitOtherCountryResidencyIsNotRegressedByPRAlias(t *testing.T) {
	profile := seekLegacyPRProfile()
	profile.ApplicationDefaults.WorkRights = []domain.ApplicationWorkRight{{Country: "New Zealand", Status: "yes", VisaType: "permanent resident"}}
	field := SnapField{Ref: "f0", Label: "Describe your current right to work in New Zealand", Type: SnapTypeTextarea, Required: true}
	proposal := LLMFieldProposal{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "I am a permanent resident", Evidence: []ProfileEvidence{{Path: "/application_defaults/work_rights/0/visa_type", Quote: "permanent resident"}}}
	_, compatible := prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.True(t, compatible)
	profile.ApplicationDefaults.WorkRights[0].VisaType = "PR"
	proposal.Evidence[0].Quote = "PR"
	_, compatible = prepareGroundedCandidate(profile, makeSnap(field), &proposal)
	require.False(t, compatible, "the new abbreviation rule is limited to its verified AU context")
}
