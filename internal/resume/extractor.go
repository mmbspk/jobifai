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
	profile, _, err := e.ExtractWithEvidence(ctx, resumeText)
	return profile, err
}

// ExtractWithEvidence extracts the profile and optional application-answer
// suggestions in the same metered call. No model-supplied application default
// survives unless its cited statement passes deterministic evidence validation.
func (e *Extractor) ExtractWithEvidence(ctx context.Context, resumeText string) (*domain.ResumeProfile, []domain.CVAnswerPrefill, error) {
	in, _ := json.Marshal(map[string]string{"resume_text": resumeText})
	msgs, err := ProviderMessages(domain.TaskResumeExtract, in)
	if err != nil {
		return nil, nil, fmt.Errorf("extract resume: %w", err)
	}
	raw, err := e.client.Chat(llm.WithTask(ctx, "extract resume"), msgs)
	if err != nil {
		return nil, nil, fmt.Errorf("extract resume: %w", err)
	}

	raw = strings.TrimSpace(raw)
	raw = stripFence(raw)

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		// The response may contain private CV text; never include it in errors.
		return nil, nil, fmt.Errorf("extract resume: invalid JSON from LLM")
	}
	delete(fields, "application_defaults")
	delete(fields, "prompt_instructions")
	profileRaw, _ := json.Marshal(fields)
	var profile domain.ResumeProfile
	if err := json.Unmarshal(profileRaw, &profile); err != nil {
		return nil, nil, fmt.Errorf("extract resume: invalid profile JSON from LLM")
	}
	var envelope struct {
		Prefill []json.RawMessage `json:"application_answer_prefill"`
	}
	// Malformed sidecar items are isolated from the ordinary profile import.
	_ = json.Unmarshal([]byte(raw), &envelope)
	profile.ApplicationDefaults = domain.ApplicationDefaults{}
	profile.PromptInstructions = ""
	prefill := validateCVAnswers(resumeText, envelope.Prefill, &profile.ApplicationDefaults)
	return &profile, prefill, nil
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
