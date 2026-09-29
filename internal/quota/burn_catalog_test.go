package quota_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
)

func TestEstimateBurnCreditsCatalog_MatchesPostCallPricing(t *testing.T) {
	t.Parallel()
	catalog := pricing.DefaultCatalog()
	def := domain.QuotaDefaults{CreditsPerUSD: 1000, ServiceMarkup: 0.5}
	models := []string{"claude-haiku-4-5", "claude-sonnet-4-6", "claude-sonnet-5-5"}
	for _, model := range models {
		est := quota.EstimateBurnCreditsCatalog(def, catalog, model, 10_000, 2000)
		res, err := catalog.Resolve(model, false)
		assert.NoError(t, err)
		raw := pricing.RawCostMicroUSD(res.Record, pricing.TokenUsage{InputTokens: 10_000, OutputTokens: 2000})
		post := quota.BurnCreditsFromRawMicro(def, raw, true)
		assert.Equal(t, post, est, model)
	}
}
