package resume

import (
	"encoding/json"
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// ProviderMessages builds the same llm.Message slice production services send for a task input payload.
func ProviderMessages(task string, input json.RawMessage) ([]llm.Message, error) {
	switch task {
	case domain.TaskJobScoring:
		var in struct {
			Profile        json.RawMessage `json:"profile"`
			JobDescription string          `json:"job_description"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		var profile domain.ResumeProfile
		_ = json.Unmarshal(in.Profile, &profile)
		p, err := BuildJobScoringPrompt(&profile, in.JobDescription)
		if err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "user", Content: p}}, nil
	case domain.TaskEmploymentEthics:
		var in struct{ Title, Company, Description string }
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		p, err := BuildHalalPrompt(in.Title, in.Company, in.Description)
		if err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "user", Content: p}}, nil
	case domain.TaskFormAnswer:
		var in struct {
			ProfileJSON json.RawMessage `json:"profile_json"`
			Question    string          `json:"question"`
			Options     []string        `json:"options"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		p := BuildFormAnswerPrompt(in.ProfileJSON, in.Question, in.Options)
		return []llm.Message{{Role: "user", Content: p}}, nil
	case domain.TaskFormVision:
		return []llm.Message{{Role: "user", Content: FormVisionIdentifyPrompt}}, nil
	case domain.TaskResumeExtract:
		var in struct{ ResumeText string `json:"resume_text"` }
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "user", Content: BuildExtractPrompt(in.ResumeText)}}, nil
	case domain.TaskResumeTailoring:
		var in struct {
			Profile        json.RawMessage `json:"profile"`
			JobDescription string          `json:"job_description"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		var profile domain.ResumeProfile
		if err := json.Unmarshal(in.Profile, &profile); err != nil {
			return nil, err
		}
		p, err := BuildTailorPrompt(&profile, in.JobDescription)
		if err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "user", Content: p}}, nil
	case domain.TaskCoverLetter:
		var in struct {
			Profile        json.RawMessage `json:"profile"`
			JobDescription string          `json:"job_description"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		var profile domain.ResumeProfile
		if err := json.Unmarshal(in.Profile, &profile); err != nil {
			return nil, err
		}
		p, err := BuildCoverLetterPrompt(&profile, in.JobDescription)
		if err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "user", Content: p}}, nil
	case domain.TaskApplicationQuestions:
		var in struct {
			Profile    json.RawMessage   `json:"profile"`
			JobContext string            `json:"job_context"`
			Questions  []json.RawMessage `json:"questions"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		var profile domain.ResumeProfile
		if err := json.Unmarshal(in.Profile, &profile); err != nil {
			return nil, err
		}
		qs := applicationQuestionTexts(in.Questions)
		sys, user, err := BuildApplicationQuestionsPrompt(&profile, in.JobContext, qs)
		if err != nil {
			return nil, err
		}
		return []llm.Message{{Role: "system", Content: sys}, {Role: "user", Content: user}}, nil
	default:
		return nil, fmt.Errorf("unsupported task %q", task)
	}
}

func applicationQuestionTexts(raw []json.RawMessage) []string {
	qs := make([]string, 0, len(raw))
	for _, q := range raw {
		var obj struct{ Text string `json:"text"` }
		if json.Unmarshal(q, &obj) == nil && obj.Text != "" {
			qs = append(qs, obj.Text)
			continue
		}
		qs = append(qs, string(q))
	}
	return qs
}
