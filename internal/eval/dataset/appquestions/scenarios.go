package appquestions

import (
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
)

// Scenario is one application-questions eval case with typed profile data.
type Scenario struct {
	Tag       string
	JobCtx    string
	Questions []Question
	Expect    []AnswerExpect
	Profile   domain.ResumeProfile
}

type Question struct {
	ID   string
	Text string
}

type AnswerExpect struct {
	Question string
	Match    string
	Value    string
	Contains string
}

func Scenarios() []Scenario {
	return []Scenario{
		{
			Tag: "sponsor_notice", JobCtx: "Regional logistics company",
			Profile: profile(true, "8 weeks", "95000"),
			Questions: []Question{
				{ID: "sponsor", Text: "Do you require employer sponsorship?"},
				{ID: "notice", Text: "What is your notice period?"},
			},
			Expect: []AnswerExpect{
				{Question: "Do you require employer sponsorship?", Match: "boolean_yes", Value: "Yes"},
				{Question: "What is your notice period?", Match: "duration", Value: "8 weeks"},
			},
		},
		{
			Tag: "years_and_salary", JobCtx: "Hospital administration",
			Profile: profileWithEmploymentPeriod(false, "4 weeks", "88000", "2019 – 2026"),
			Questions: []Question{
				{ID: "years", Text: "How many years of nursing experience do you have?"},
				{ID: "salary", Text: "Expected salary (AUD)?"},
			},
			Expect: []AnswerExpect{
				{Question: "How many years of nursing experience do you have?", Match: "years_count", Value: "7"},
				{Question: "Expected salary (AUD)?", Match: "currency_amount", Value: "88000"},
			},
		},
		{
			Tag: "notice_only", JobCtx: "Education provider",
			Profile: profile(false, "6 weeks", "80000"),
			Questions: []Question{
				{ID: "notice", Text: "What is your notice period with your current employer?"},
			},
			Expect: []AnswerExpect{
				{Question: "What is your notice period with your current employer?", Match: "duration", Value: "6 weeks"},
			},
		},
		{
			Tag: "sponsorship_required", JobCtx: "Construction group",
			Profile: profile(true, "12 weeks", "105000"),
			Questions: []Question{
				{ID: "sponsor", Text: "Will you now or in the future require visa sponsorship?"},
			},
			Expect: []AnswerExpect{
				{Question: "Will you now or in the future require visa sponsorship?", Match: "boolean_yes", Value: "Yes"},
			},
		},
		{
			Tag: "salary_expectation", JobCtx: "Public sector agency",
			Profile: profile(false, "4 weeks", "112000"),
			Questions: []Question{
				{ID: "salary", Text: "What are your salary expectations (AUD)?"},
			},
			Expect: []AnswerExpect{
				{Question: "What are your salary expectations (AUD)?", Match: "currency_amount", Value: "112000"},
			},
		},
		{
			Tag: "employment_period", JobCtx: "Community pharmacy",
			Profile: profile(false, "2 weeks", "72000"),
			Questions: []Question{
				{ID: "start", Text: "When did you start your current nursing role?"},
			},
			Expect: []AnswerExpect{
				{Question: "When did you start your current nursing role?", Match: "contains", Contains: "2019"},
			},
		},
		{
			Tag: "sponsor_no", JobCtx: "Local council",
			Profile: profile(false, "3 weeks", "91000"),
			Questions: []Question{
				{ID: "sponsor", Text: "Do you require employer sponsorship?"},
			},
			Expect: []AnswerExpect{
				{Question: "Do you require employer sponsorship?", Match: "boolean_no", Value: "No"},
			},
		},
		{
			Tag: "notice_and_salary", JobCtx: "Operations consultancy",
			Profile: profile(false, "4 weeks", "99000"),
			Questions: []Question{
				{ID: "notice", Text: "What is your notice period?"},
				{ID: "salary", Text: "Expected salary (AUD)?"},
			},
			Expect: []AnswerExpect{
				{Question: "What is your notice period?", Match: "duration", Value: "4 weeks"},
				{Question: "Expected salary (AUD)?", Match: "currency_amount", Value: "99000"},
			},
		},
	}
}

func profile(sponsor bool, notice, salary string) domain.ResumeProfile {
	return profileWithEmploymentPeriod(sponsor, notice, salary, "2019 – Present")
}

func profileWithEmploymentPeriod(sponsor bool, notice, salary, period string) domain.ResumeProfile {
	return domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Casey", Surname: "Nguyen"},
		ApplicationDefaults: domain.ApplicationDefaults{
			RequiresSponsorship: sponsor,
			NoticePeriod:        notice,
			SalaryExpectation:   salary,
		},
		ExperienceDetails: []domain.ExperienceDetail{{
			Position: "Registered Nurse", Company: "Regional Health", EmploymentPeriod: period,
		}},
	}
}

// ProfileJSON marshals the profile for dataset input (production shape).
func ProfileJSON(p domain.ResumeProfile) (json.RawMessage, error) {
	return json.Marshal(p)
}
