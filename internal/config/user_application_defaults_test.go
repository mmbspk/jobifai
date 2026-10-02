package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func TestApplyUnsetUserApplicationFields_LegacySparseTrialRow(t *testing.T) {
	sparse := domain.GeneralSettings{
		HumanBehavior: domain.HumanBehaviorConfig{DailyApplicationLimit: 5},
	}
	got := config.ApplyUnsetUserApplicationFields(sparse)
	assert.Equal(t, 7, got.JobSuitabilityScore)
	assert.Equal(t, 25, got.MaxJobsPerKeyword)
	assert.True(t, got.RequireReview)
	assert.Equal(t, 5, got.HumanBehavior.DailyApplicationLimit)
}

func TestApplyUnsetUserApplicationFields_PreservesExplicitReviewOff(t *testing.T) {
	custom := domain.GeneralSettings{
		RequireReview:       false,
		JobSuitabilityScore: 8,
		MaxJobsPerKeyword:   10,
	}
	got := config.ApplyUnsetUserApplicationFields(custom)
	assert.False(t, got.RequireReview)
	assert.Equal(t, 8, got.JobSuitabilityScore)
}
