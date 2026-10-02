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

type stubScorer struct {
	result domain.JobScore
	err    error
}

func (s stubScorer) EvaluateJob(context.Context, *domain.ResumeProfile, string) (domain.JobScore, error) {
	return s.result, s.err
}

type stubHalal struct {
	err error
}

func (h stubHalal) CheckHalal(context.Context, string, string, string) (domain.HalalVerdict, error) {
	return domain.HalalVerdict{}, h.err
}

func TestCheckSeekScore_LLMFailureDoesNotPassJob(t *testing.T) {
	b := New(Config{
		Scorer: stubScorer{err: fmt.Errorf("scorer: llm: %w", quota.ErrExceeded)},
	})
	job := seekJob{ID: "1", Title: "Lecturer", Company: "Uni", URL: "https://seek.example/job/1"}

	_, _, _, ok := b.checkSeekScore(context.Background(), job, "job description")
	assert.False(t, ok)
	select {
	case <-b.stopCh:
	default:
		t.Fatal("expected bot stopped after scorer quota failure")
	}
}

func TestCheckSeekScore_HalalQuotaFailureDoesNotPassJob(t *testing.T) {
	b := New(Config{
		Scorer:       stubScorer{result: domain.JobScore{Score: 9, Reasoning: "good"}},
		HalalChecker: stubHalal{err: fmt.Errorf("halal: llm: %w", quota.ErrExceeded)},
	})
	job := seekJob{ID: "2", Title: "Lecturer", Company: "Uni", URL: "https://seek.example/job/2"}

	score, _, _, ok := b.checkSeekScore(context.Background(), job, "job description")
	require.False(t, ok)
	assert.Equal(t, 9, score)
	select {
	case <-b.stopCh:
	default:
		t.Fatal("expected bot stopped after halal quota failure")
	}
}
