package quota

import "github.com/user/jobifai/internal/domain"

// BurnCredits computes loaded credits to deduct after an LLM call.
func BurnCredits(def domain.QuotaDefaults, model string, inputTokens, outputTokens int64) int64 {
	llmMicro := MicroUSDFromTokens(model, inputTokens, outputTokens)
	llmUSD := USDFromMicro(llmMicro)
	if llmUSD <= 0 && inputTokens+outputTokens == 0 {
		return 0
	}
	markup := def.ServiceMarkup
	if markup < 0 {
		markup = 0
	}
	creditsPerUSD := def.CreditsPerUSD
	if creditsPerUSD <= 0 {
		creditsPerUSD = 1000
	}
	loadedUSD := llmUSD*(1+markup) + def.PerCallFeeUSD
	if loadedUSD < 0 {
		loadedUSD = 0
	}
	credits := int64(loadedUSD * creditsPerUSD)
	if credits < 1 && (llmMicro > 0 || def.PerCallFeeUSD > 0) {
		return 1
	}
	return credits
}

// EstimateBurnCredits is used for pre-call quota checks.
func EstimateBurnCredits(def domain.QuotaDefaults, model string, estInput, estOutput int) int64 {
	return BurnCredits(def, model, int64(estInput), int64(estOutput))
}
