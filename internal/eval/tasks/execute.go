package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

// Execute runs one eval case through production prompt builders.
func Execute(ctx context.Context, task string, client *llm.Client, c dataset.Case) (string, error) {
	switch task {
	case domain.TaskJobScoring:
		return runScoring(ctx, client, c)
	case domain.TaskEmploymentEthics:
		return runEthics(ctx, client, c)
	case domain.TaskFormAnswer:
		return runFormAnswer(ctx, client, c)
	case domain.TaskFormVision:
		return runFormVision(ctx, client, c)
	case domain.TaskResumeExtract:
		return runExtract(ctx, client, c)
	case domain.TaskResumeTailoring:
		return runTailor(ctx, client, c)
	case domain.TaskCoverLetter:
		return runCover(ctx, client, c)
	case domain.TaskApplicationQuestions:
		return runQuestions(ctx, client, c)
	default:
		return "", fmt.Errorf("unsupported task %q", task)
	}
}

func runScoring(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		Profile        json.RawMessage `json:"profile"`
		JobDescription string          `json:"job_description"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	var profile domain.ResumeProfile
	_ = json.Unmarshal(in.Profile, &profile)
	prompt, err := resume.BuildJobScoringPrompt(&profile, in.JobDescription)
	if err != nil {
		return "", err
	}
	return client.Chat(llm.WithTask(ctx, domain.TaskJobScoring), []llm.Message{{Role: "user", Content: prompt}})
}

func runEthics(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		Title, Company, Description string
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	prompt, err := resume.BuildHalalPrompt(in.Title, in.Company, in.Description)
	if err != nil {
		return "", err
	}
	return client.Chat(llm.WithTask(ctx, domain.TaskEmploymentEthics), []llm.Message{{Role: "user", Content: prompt}})
}

func runFormAnswer(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		ProfileJSON json.RawMessage `json:"profile_json"`
		Question    string          `json:"question"`
		Options     []string        `json:"options"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	prompt := resume.BuildFormAnswerPrompt(in.ProfileJSON, in.Question, in.Options)
	return client.Chat(llm.WithTask(ctx, domain.TaskFormAnswer), []llm.Message{{Role: "user", Content: prompt}})
}

func runFormVision(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		FixturePNG string `json:"fixture_png"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	img, err := dataset.ReadFixtureBytes(c, in.FixturePNG)
	if err != nil {
		return "", err
	}
	return client.ChatWithImage(llm.WithTask(ctx, domain.TaskFormVision), img, resume.FormVisionIdentifyPrompt)
}

func runExtract(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		ResumeText string `json:"resume_text"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	prompt := resume.BuildExtractPrompt(in.ResumeText)
	return client.Chat(llm.WithTask(ctx, domain.TaskResumeExtract), []llm.Message{{Role: "user", Content: prompt}})
}

func runTailor(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		Profile        json.RawMessage `json:"profile"`
		JobDescription string          `json:"job_description"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	var profile domain.ResumeProfile
	if err := json.Unmarshal(in.Profile, &profile); err != nil {
		return "", err
	}
	prompt, err := resume.BuildTailorPrompt(&profile, in.JobDescription)
	if err != nil {
		return "", err
	}
	return client.Chat(llm.WithTask(ctx, domain.TaskResumeTailoring), []llm.Message{{Role: "user", Content: prompt}})
}

func runCover(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		Profile        json.RawMessage `json:"profile"`
		JobDescription string          `json:"job_description"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	var profile domain.ResumeProfile
	if err := json.Unmarshal(in.Profile, &profile); err != nil {
		return "", err
	}
	prompt, err := resume.BuildCoverLetterPrompt(&profile, in.JobDescription)
	if err != nil {
		return "", err
	}
	return client.Chat(llm.WithTask(ctx, domain.TaskCoverLetter), []llm.Message{{Role: "user", Content: prompt}})
}

func runQuestions(ctx context.Context, client *llm.Client, c dataset.Case) (string, error) {
	var in struct {
		Profile    json.RawMessage `json:"profile"`
		JobContext string          `json:"job_context"`
		Questions  []string        `json:"questions"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return "", err
	}
	var profile domain.ResumeProfile
	if err := json.Unmarshal(in.Profile, &profile); err != nil {
		return "", err
	}
	sys, user, err := resume.BuildApplicationQuestionsPrompt(&profile, in.JobContext, in.Questions)
	if err != nil {
		return "", err
	}
	return client.Chat(llm.WithTask(ctx, domain.TaskApplicationQuestions), []llm.Message{
		{Role: "system", Content: sys}, {Role: "user", Content: user},
	})
}
