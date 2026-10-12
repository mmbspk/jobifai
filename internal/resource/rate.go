package resource

import (
	"math"

	"github.com/user/jobifai/internal/domain"
)

// EstimateCredits converts a raw USD cost (in micro-USD, i.e. 1 USD = 1_000_000) to credits
// using the existing economic formula:
//
//	loaded_usd = raw_usd × (1 + serviceMarkup)
//	credits    = ceil(loaded_usd × creditsPerUSD)
//
// PerCallFeeUSD is intentionally NOT applied here — it is an AI-call-specific charge.
// Returns at least 1 credit when rawCostUSDMicro > 0 and the meter is enabled.
func EstimateCredits(rawCostUSDMicro int64, serviceMarkup, creditsPerUSD float64) int64 {
	if rawCostUSDMicro <= 0 || creditsPerUSD <= 0 {
		return 0
	}
	rawUSD := float64(rawCostUSDMicro) / 1_000_000
	loadedUSD := rawUSD * (1 + serviceMarkup)
	credits := math.Ceil(loadedUSD * creditsPerUSD)
	if credits < 1 {
		return 1
	}
	return int64(credits)
}

// LoadedCostMicro converts a raw micro-USD cost to the loaded micro-USD cost.
func LoadedCostMicro(rawCostUSDMicro int64, serviceMarkup float64) int64 {
	if rawCostUSDMicro <= 0 {
		return 0
	}
	return int64(math.Round(float64(rawCostUSDMicro) * (1 + serviceMarkup)))
}

// EstimateRenderCredits returns the credit cost for one successful PDF render using
// the current admin configuration.
func EstimateRenderCredits(res domain.ResourceMeteringDefaults, quota domain.QuotaDefaults) int64 {
	if !res.Enabled || !res.Render.Enabled || res.Render.RawCostUSDPerRender <= 0 {
		return 0
	}
	rawMicro := int64(math.Round(res.Render.RawCostUSDPerRender * 1_000_000))
	return EstimateCredits(rawMicro, quota.ServiceMarkup, quota.CreditsPerUSD)
}

// EstimateBrowserIncrementCredits returns the credit cost for one configured billing
// increment of active browser automation.
func EstimateBrowserIncrementCredits(res domain.ResourceMeteringDefaults, quota domain.QuotaDefaults) int64 {
	if !res.Enabled || !res.Browser.Enabled || res.Browser.RawCostUSDPerHour <= 0 {
		return 0
	}
	inc := res.Browser.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	rawPerHour := res.Browser.RawCostUSDPerHour
	rawPerIncrement := rawPerHour * (float64(inc) / 3600)
	rawMicro := int64(math.Round(rawPerIncrement * 1_000_000))
	if rawMicro <= 0 {
		return 0
	}
	return EstimateCredits(rawMicro, quota.ServiceMarkup, quota.CreditsPerUSD)
}

// RenderCostMicros returns (rawMicro, loadedMicro) for one PDF render.
func RenderCostMicros(res domain.ResourceMeteringDefaults) (raw, loaded int64) {
	if !res.Enabled || !res.Render.Enabled || res.Render.RawCostUSDPerRender <= 0 {
		return 0, 0
	}
	raw = int64(math.Round(res.Render.RawCostUSDPerRender * 1_000_000))
	loaded = LoadedCostMicro(raw, 0) // caller applies markup via EstimateCredits
	return raw, raw
}

// BrowserIncrementCostMicros returns (rawMicro, loadedMicro) for one billing increment.
func BrowserIncrementCostMicros(res domain.ResourceMeteringDefaults) (raw, loaded int64) {
	if !res.Enabled || !res.Browser.Enabled || res.Browser.RawCostUSDPerHour <= 0 {
		return 0, 0
	}
	inc := res.Browser.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	rawPerHour := res.Browser.RawCostUSDPerHour
	rawPerIncrement := rawPerHour * (float64(inc) / 3600)
	raw = int64(math.Round(rawPerIncrement * 1_000_000))
	return raw, raw
}
