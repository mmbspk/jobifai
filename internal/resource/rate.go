package resource

import (
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// ResourcePricing holds the computed cost for one resource event.
// Zero value means metering is disabled or not applicable for this resource type.
type ResourcePricing struct {
	RawCostUSDMicro    int64
	LoadedCostUSDMicro int64
	Credits            int64
}

// PriceResource is the single authoritative function for computing resource costs.
// Returns a zero ResourcePricing when metering is disabled or the resource type is unknown.
// PerCallFeeUSD is intentionally NOT applied — it is an AI-call-specific charge.
func PriceResource(cfg domain.ResourceMeteringDefaults, qd domain.QuotaDefaults, rt domain.ResourceType) ResourcePricing {
	if !cfg.Enabled {
		return ResourcePricing{}
	}
	switch rt {
	case domain.ResourceTypeDocumentRender:
		return priceRender(cfg.Render, qd)
	case domain.ResourceTypeBrowserAutomation:
		return priceBrowserIncrement(cfg.Browser, qd)
	default:
		return ResourcePricing{}
	}
}

func priceRender(r domain.RenderMeteringConfig, qd domain.QuotaDefaults) ResourcePricing {
	if !r.Enabled || r.RawCostUSDMicroPerRender <= 0 {
		return ResourcePricing{}
	}
	return pricingFromRaw(r.RawCostUSDMicroPerRender, qd)
}

func priceBrowserIncrement(b domain.BrowserMeteringConfig, qd domain.QuotaDefaults) ResourcePricing {
	if !b.Enabled || b.RawCostUSDMicroPerHour <= 0 {
		return ResourcePricing{}
	}
	inc := b.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	// Integer arithmetic: rawMicro = rawMicroPerHour × inc / 3600.
	// Truncation is intentional — it avoids sub-increment accumulation.
	raw := b.RawCostUSDMicroPerHour * int64(inc) / 3600
	if raw <= 0 {
		return ResourcePricing{}
	}
	return pricingFromRaw(raw, qd)
}

func pricingFromRaw(rawMicro int64, qd domain.QuotaDefaults) ResourcePricing {
	if qd.CreditsPerUSD <= 0 {
		return ResourcePricing{}
	}
	markup := qd.ServiceMarkup
	if markup < 0 {
		markup = 0
	}
	// Mirror BurnCreditsFromRawMicro (minus PerCallFeeUSD, which is AI-call-specific).
	// float64 throughout matches the AI-billing pipeline; CreditsFromLoadedUSD is shared.
	loadedUSD := float64(rawMicro) / 1_000_000 * (1 + markup)
	if loadedUSD <= 0 {
		loadedUSD = float64(rawMicro) / 1_000_000
	}
	loadedMicro := int64(loadedUSD * 1_000_000)
	if loadedMicro <= 0 {
		loadedMicro = rawMicro
	}
	credits := quota.CreditsFromLoadedUSD(qd, loadedMicro)
	return ResourcePricing{
		RawCostUSDMicro:    rawMicro,
		LoadedCostUSDMicro: loadedMicro,
		Credits:            credits,
	}
}

// EstimateRenderCredits returns the credit cost for one new PDF render at current rates.
func EstimateRenderCredits(res domain.ResourceMeteringDefaults, quota domain.QuotaDefaults) int64 {
	return PriceResource(res, quota, domain.ResourceTypeDocumentRender).Credits
}

// EstimateBrowserIncrementCredits returns the credit cost for one billing increment of
// active browser automation.
func EstimateBrowserIncrementCredits(res domain.ResourceMeteringDefaults, quota domain.QuotaDefaults) int64 {
	return PriceResource(res, quota, domain.ResourceTypeBrowserAutomation).Credits
}
