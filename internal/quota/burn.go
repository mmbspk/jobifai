package quota

import "github.com/user/jobifai/internal/domain"

// CreditsFromLoadedUSD converts loaded cost micro-USD to credits (same rules as BurnCredits tail).
func CreditsFromLoadedUSD(def domain.QuotaDefaults, loadedMicro int64) int64 {
	loadedUSD := USDFromMicro(loadedMicro)
	creditsPerUSD := def.CreditsPerUSD
	if creditsPerUSD <= 0 {
		creditsPerUSD = 1000
	}
	credits := int64(loadedUSD * float64(creditsPerUSD))
	if credits < 1 && loadedMicro > 0 {
		return 1
	}
	return credits
}

// BurnCreditsFromRawMicro applies markup + per-call fee to raw provider micro-USD.
func BurnCreditsFromRawMicro(def domain.QuotaDefaults, rawMicro int64, includeCallFee bool) int64 {
	llmUSD := USDFromMicro(rawMicro)
	markup := def.ServiceMarkup
	if markup < 0 {
		markup = 0
	}
	loadedUSD := llmUSD * (1 + markup)
	if includeCallFee {
		loadedUSD += def.PerCallFeeUSD
	}
	if loadedUSD < 0 {
		loadedUSD = 0
	}
	return CreditsFromLoadedUSD(def, int64(loadedUSD*1_000_000))
}

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
	loadedUSD := llmUSD*(1+markup) + def.PerCallFeeUSD
	if loadedUSD < 0 {
		loadedUSD = 0
	}
	return CreditsFromLoadedUSD(def, int64(loadedUSD*1_000_000))
}

// EstimateBurnCredits is used for pre-call quota checks.
func EstimateBurnCredits(def domain.QuotaDefaults, model string, estInput, estOutput int) int64 {
	return BurnCredits(def, model, int64(estInput), int64(estOutput))
}
