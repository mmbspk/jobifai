package formanswer

import (
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

// Scenario is one LLM form-answer benchmark case (post-heuristic).
type Scenario struct {
	Tag      string
	Question string
	Options  []string
	Expect   string
	Profile  domain.ResumeProfile
}

// LLMScenarios returns eval cases that reach Tailor.AnswerFormQuestion in production.
func LLMScenarios() []Scenario {
	return []Scenario{
		{
			Tag: "epic_ehr_skill", Question: "Do you have hands-on experience with Epic EHR?", Options: []string{"Yes", "No"}, Expect: "Yes",
			Profile: baseProfile(
				[]string{"Epic EHR", "patient scheduling"},
				nil, nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "cloud_platform_select", Question: "Which cloud platform have you used most in production?", Options: []string{"AWS", "Azure", "GCP"}, Expect: "AWS",
			Profile: baseProfile(
				[]string{"AWS", "Terraform"},
				nil, nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "healthcare_industry", Question: "Have you worked in the healthcare industry?", Options: []string{"Yes", "No"}, Expect: "Yes",
			Profile: baseProfile(
				[]string{"care coordination"},
				[]domain.ExperienceDetail{{Position: "Care Coordinator", Company: "Metro Health", EmploymentPeriod: "2018 – Present", Industry: "Healthcare"}},
				nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "language_proficiency", Question: "What is your proficiency in German?", Options: []string{"Basic", "Professional", "Native"}, Expect: "Professional",
			Profile: baseProfile(
				nil, nil,
				[]domain.Language{{Language: "German", Proficiency: "Professional"}},
				domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "education_field", Question: "What is your highest qualification in nursing?", Options: nil, Expect: "Bachelor",
			Profile: withEducation(baseProfile(nil, nil, nil, domain.ApplicationDefaults{}),
				domain.EducationDetail{EducationLevel: "Bachelor", FieldOfStudy: "Nursing"}),
		},
		{
			Tag: "salary_expectation_llm", Question: "What is your expected annual salary (AUD)?", Options: nil, Expect: "95000",
			Profile: baseProfile(nil, nil, nil, domain.ApplicationDefaults{SalaryExpectation: "95000"}),
		},
		{
			Tag: "notice_period_llm", Question: "What notice period would you need to give your current employer?", Options: nil, Expect: "4 weeks",
			Profile: baseProfile(nil, nil, nil, domain.ApplicationDefaults{NoticePeriod: "4 weeks"}),
		},
		{
			Tag: "motivation_snippet", Question: "In one sentence, why are you interested in this role?", Options: nil, Expect: "community programs",
			Profile: func() domain.ResumeProfile {
				p := baseProfile([]string{"program delivery", "community programs"}, nil, nil, domain.ApplicationDefaults{})
				return p
			}(),
		},
		{
			Tag: "sap_exposure", Question: "Have you configured SAP modules before?", Options: []string{"Yes", "No"}, Expect: "Yes",
			Profile: baseProfile(
				[]string{"SAP MM", "procurement"},
				[]domain.ExperienceDetail{{Position: "Systems Analyst", Company: "Supply Co", EmploymentPeriod: "2016 – Present"}},
				nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "presentation_skill", Question: "Are you comfortable presenting findings to senior stakeholders?", Options: []string{"Yes", "No"}, Expect: "Yes",
			Profile: baseProfile(
				[]string{"stakeholder presentations", "reporting"},
				[]domain.ExperienceDetail{{Position: "Analyst", Company: "Policy Group", EmploymentPeriod: "2019 – Present", KeyResponsibilities: []string{"Presented quarterly reviews to directors"}}},
				nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "python_skill_years_phrase", Question: "Describe your Python experience for data analysis.", Options: nil, Expect: "Python",
			Profile: baseProfile(
				[]string{"Python", "pandas", "SQL"},
				nil, nil, domain.ApplicationDefaults{},
			),
		},
		{
			Tag: "work_arrangement_preference", Question: "Which work arrangement do you prefer?", Options: []string{"Remote", "Hybrid", "Onsite"}, Expect: "Hybrid",
			Profile: baseProfile([]string{"hybrid collaboration", "Hybrid"}, nil, nil, domain.ApplicationDefaults{}),
		},
	}
}

func baseProfile(skills []string, exp []domain.ExperienceDetail, langs []domain.Language, ad domain.ApplicationDefaults) domain.ResumeProfile {
	return domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Sam", Surname: "Reed", City: "Sydney"},
		Skills:              skills,
		ExperienceDetails:   exp,
		Languages:           langs,
		ApplicationDefaults: ad,
	}
}

func withEducation(p domain.ResumeProfile, e domain.EducationDetail) domain.ResumeProfile {
	p.EducationDetails = []domain.EducationDetail{e}
	return p
}

// FormFillingJSON returns the profile JSON production sends to AnswerFormQuestion.
func FormFillingJSON(p domain.ResumeProfile) (json.RawMessage, error) {
	trimmed := resume.ForFormFilling(&p)
	return json.Marshal(trimmed)
}
