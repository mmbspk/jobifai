package tasks

import (
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/resume"
)

const messageOverheadTokens int64 = 12

// ConservativeBillableTokens returns a fail-closed upper bound on billable tokens for one eval call.
func ConservativeBillableTokens(task string, c dataset.Case, maxOutputTokens int) pricing.TokenUsage {
	if maxOutputTokens <= 0 {
		maxOutputTokens = 8192
	}
	if task == domain.TaskFormVision {
		return conservativeVisionBillable(c, maxOutputTokens)
	}
	msgs, err := MessagesForCaseOrError(task, c)
	if err != nil {
		return pricing.TokenUsage{InputTokens: 4096, OutputTokens: int64(maxOutputTokens)}
	}
	var chars int
	for _, m := range msgs {
		chars += len(m.Content) + len(m.Role)
	}
	estIn := charsToInputTokens(chars) + int64(len(msgs))*messageOverheadTokens
	return pricing.TokenUsage{InputTokens: estIn, OutputTokens: int64(maxOutputTokens)}
}

func conservativeVisionBillable(c dataset.Case, maxOutputTokens int) pricing.TokenUsage {
	var in struct {
		FixturePNG string `json:"fixture_png"`
	}
	_ = json.Unmarshal(c.Input, &in)
	imgLen := 0
	if b, err := dataset.ReadFixtureBytes(c, in.FixturePNG); err == nil {
		imgLen = len(b)
	}
	promptChars := len(resume.FormVisionIdentifyPrompt)
	estIn := visionInputTokens(int64(imgLen), int64(promptChars))
	return pricing.TokenUsage{InputTokens: estIn, OutputTokens: int64(maxOutputTokens)}
}

func charsToInputTokens(chars int) int64 {
	est := int64(chars / 4)
	if est < 1 {
		est = 1
	}
	return est
}

func visionInputTokens(imageLen, promptChars int64) int64 {
	est := imageLen/4 + promptChars/4
	if est < 1500 {
		est = 1500
	}
	return est
}
