package tasks

import (
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/resume"
)

const messageOverheadTokens int64 = 12

// ConservativeBillableTokens returns a deliberately conservative upper bound on billable input
// tokens for one eval call (reservations reconcile after the request; over-reserving is OK).
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
	var bytes int
	for _, m := range msgs {
		bytes += len([]byte(m.Content)) + len([]byte(m.Role))
	}
	estIn := utf8ByteInputTokenUpperBound(bytes) + int64(len(msgs))*messageOverheadTokens
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
	promptBytes := len([]byte(resume.FormVisionIdentifyPrompt))
	estIn := visionInputTokenUpperBound(int64(imgLen), int64(promptBytes))
	return pricing.TokenUsage{InputTokens: estIn, OutputTokens: int64(maxOutputTokens)}
}

// utf8ByteInputTokenUpperBound assumes at most one billable input token per UTF-8 byte (safe for ASCII and multibyte text).
func utf8ByteInputTokenUpperBound(totalBytes int) int64 {
	est := int64(totalBytes)
	if est < 1 {
		est = 1
	}
	return est
}

func visionInputTokenUpperBound(imageBytes, promptBytes int64) int64 {
	est := imageBytes + promptBytes
	if est < 1500 {
		est = 1500
	}
	return est
}
