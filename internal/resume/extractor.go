// Package resume provides resume extraction and generation services.
package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

// Extractor uses an LLM to parse raw resume text into a ResumeProfile.
type Extractor struct {
	client *llm.Client
}

func NewExtractor(client *llm.Client) *Extractor {
	return &Extractor{client: client}
}

// ExtractFromText calls the LLM to parse resume text into a structured profile.
func (e *Extractor) ExtractFromText(ctx context.Context, resumeText string) (*domain.ResumeProfile, error) {
	in, _ := json.Marshal(map[string]string{"resume_text": resumeText})
	msgs, err := ProviderMessages(domain.TaskResumeExtract, in)
	if err != nil {
		return nil, fmt.Errorf("extract resume: %w", err)
	}
	raw, err := e.client.Chat(llm.WithTask(ctx, "extract resume"), msgs)
	if err != nil {
		return nil, fmt.Errorf("extract resume: %w", err)
	}

	raw = strings.TrimSpace(raw)
	raw = stripFence(raw)

	var profile domain.ResumeProfile
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return nil, fmt.Errorf("extract resume: invalid JSON from LLM: %w\nraw: %s", err, raw)
	}
	return &profile, nil
}

func stripFence(s string) string {
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimFunc(s, unicode.IsSpace)
}
