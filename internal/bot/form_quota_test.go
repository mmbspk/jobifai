package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

type quotaFailTailor struct{}

type providerRateLimitTailor struct{}

func (quotaFailTailor) TailorProfile(context.Context, *domain.ResumeProfile, string) (*domain.ResumeProfile, error) {
	return nil, fmt.Errorf("unexpected")
}
func (quotaFailTailor) WriteCoverLetter(context.Context, *domain.ResumeProfile, string) (string, error) {
	return "", fmt.Errorf("unexpected")
}
func (quotaFailTailor) AnswerFormQuestion(context.Context, []byte, string, []string) (string, error) {
	return "", fmt.Errorf("form answer: llm: %w", quota.ErrExceeded)
}
func (quotaFailTailor) IdentifyFormFields(context.Context, []byte) ([]domain.IdentifiedField, error) {
	return nil, nil
}

func (providerRateLimitTailor) TailorProfile(context.Context, *domain.ResumeProfile, string) (*domain.ResumeProfile, error) {
	return nil, fmt.Errorf("unexpected")
}
func (providerRateLimitTailor) WriteCoverLetter(context.Context, *domain.ResumeProfile, string) (string, error) {
	return "", fmt.Errorf("unexpected")
}
func (providerRateLimitTailor) AnswerFormQuestion(context.Context, []byte, string, []string) (string, error) {
	return "", fmt.Errorf("form answer: llm: llm http 429: rate limit exceeded")
}
func (providerRateLimitTailor) IdentifyFormFields(context.Context, []byte) ([]domain.IdentifiedField, error) {
	return nil, nil
}

func TestAnswerFormQuestion_JobifaiQuota_NoFallbackOption(t *testing.T) {
	b := &Bot{cfg: Config{Tailor: quotaFailTailor{}}}
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "job-1"}}
	ans, err := b.answerFormQuestion(context.Background(), lazy, "Describe your teaching philosophy", []string{"A", "B"})
	require.Error(t, err)
	assert.True(t, isJobifaiQuotaExceeded(err))
	assert.Empty(t, ans)
}

func TestAnswerFormQuestion_JobifaiQuota_DoesNotUseLastResortFallback(t *testing.T) {
	ans, err := resolveFormFieldAnswer("", fmt.Errorf("form question: %w", quota.ErrExceeded), "A")
	require.Error(t, err)
	assert.Empty(t, ans)
	// Same guard used by LinkedIn Easy Apply and Seek Quick Apply before Continue/Submit.
	assert.False(t, shouldAdvanceApplyForm(err))
}

func TestAnswerFormQuestion_ProviderRateLimit_NoFallbackOption(t *testing.T) {
	b := &Bot{cfg: Config{Tailor: providerRateLimitTailor{}}}
	lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "job-2"}}
	ans, err := b.answerFormQuestion(context.Background(), lazy, "Years of experience?", []string{"0-1", "2-5"})
	require.Error(t, err)
	assert.True(t, isProviderUsageLimit(err))
	assert.False(t, isJobifaiQuotaExceeded(err))
	assert.Empty(t, ans)

	resolved, resolveErr := resolveFormFieldAnswer("", err, "0-1")
	assert.Error(t, resolveErr)
	assert.Empty(t, resolved)
	assert.False(t, shouldAdvanceApplyForm(err))
	assert.Contains(t, llmAbortReason(err), "provider")
}
