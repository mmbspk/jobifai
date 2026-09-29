package quota

import (
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/pricing"
)

// EstimateBurnCreditsCatalog uses the pricing catalog (same path as post-call billing).
func EstimateBurnCreditsCatalog(def domain.QuotaDefaults, catalog *pricing.Catalog, model string, estInput, estOutput int) int64 {
	if catalog == nil {
		return EstimateBurnCredits(def, model, estInput, estOutput)
	}
	res, err := catalog.Resolve(model, false)
	if err != nil {
		return EstimateBurnCredits(def, model, estInput, estOutput)
	}
	raw := pricing.RawCostMicroUSD(res.Record, pricing.TokenUsage{
		InputTokens:  int64(estInput),
		OutputTokens: int64(estOutput),
	})
	return BurnCreditsFromRawMicro(def, raw, true)
}
