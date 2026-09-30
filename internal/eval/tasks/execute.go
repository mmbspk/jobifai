package tasks

import (
	"context"
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

// Execute runs one eval case through production prompt builders.
func Execute(ctx context.Context, task string, client *llm.Client, c dataset.Case) (string, error) {
	switch task {
	case domain.TaskFormVision:
		return runFormVision(ctx, client, c)
	default:
		msgs, err := MessagesForCaseOrError(task, c)
		if err != nil {
			return "", err
		}
		return client.Chat(llm.WithTask(ctx, task), msgs)
	}
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
