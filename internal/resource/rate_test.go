package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

func TestEstimateCredits_DisabledMeter(t *testing.T) {
	// Zero rawCostUSDMicro → 0 credits regardless of markup/rate
	assert.Equal(t, int64(0), EstimateCredits(0, 0.5, 1000))
	assert.Equal(t, int64(0), EstimateCredits(-1, 0.5, 1000))
}

func TestEstimateCredits_ZeroCreditsPerUSD(t *testing.T) {
	assert.Equal(t, int64(0), EstimateCredits(2000, 0.5, 0))
}

func TestEstimateCredits_BasicConversion(t *testing.T) {
	// raw = $0.002 (2000 micro-USD), markup = 50%, CreditsPerUSD = 1000
	// loaded = 0.002 × 1.5 = 0.003 USD
	// credits = ceil(0.003 × 1000) = ceil(3) = 3
	assert.Equal(t, int64(3), EstimateCredits(2000, 0.5, 1000))
}

func TestEstimateCredits_MarkupApplied(t *testing.T) {
	// raw = $0.001 (1000 micro-USD), markup = 100%
	// loaded = 0.001 × 2.0 = 0.002 USD
	// credits = ceil(0.002 × 1000) = 2
	assert.Equal(t, int64(2), EstimateCredits(1000, 1.0, 1000))
}

func TestEstimateCredits_NoMarkup(t *testing.T) {
	// raw = $0.001, markup = 0%
	// credits = ceil(0.001 × 1000) = 1
	assert.Equal(t, int64(1), EstimateCredits(1000, 0, 1000))
}

func TestEstimateCredits_MinimumOne(t *testing.T) {
	// raw = $0.0000001 (0.1 micro-USD): loaded × creditsPerUSD rounds to <1, but must return 1
	assert.Equal(t, int64(1), EstimateCredits(1, 0.5, 1000))
}

func TestEstimateCredits_PerCallFeeNotApplied(t *testing.T) {
	// This test documents that PerCallFeeUSD is NOT in the resource formula.
	// With identical inputs, AI burns more (PerCallFeeUSD adds extra) but resource uses only:
	// credits = ceil(raw × (1+markup) × creditsPerUSD)
	got := EstimateCredits(2000, 0.5, 1000)
	assert.Equal(t, int64(3), got, "resource must not apply PerCallFeeUSD")
}

func TestEstimateRenderCredits_DisabledMaster(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: false, Render: domain.RenderMeteringConfig{Enabled: true, RawCostUSDPerRender: 0.002}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateRenderCredits(res, q))
}

func TestEstimateRenderCredits_DisabledRender(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Render: domain.RenderMeteringConfig{Enabled: false, RawCostUSDPerRender: 0.002}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateRenderCredits(res, q))
}

func TestEstimateRenderCredits_ZeroRate(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Render: domain.RenderMeteringConfig{Enabled: true, RawCostUSDPerRender: 0}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateRenderCredits(res, q))
}

func TestEstimateRenderCredits_Calculation(t *testing.T) {
	// raw = $0.002 × 1.5 × 1000 = 3 credits
	res := domain.ResourceMeteringDefaults{Enabled: true, Render: domain.RenderMeteringConfig{Enabled: true, RawCostUSDPerRender: 0.002}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(3), EstimateRenderCredits(res, q))
}

func TestEstimateBrowserIncrementCredits_DisabledMaster(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: false, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 0.02, BillingIncrementSecs: 30}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q))
}

func TestEstimateBrowserIncrementCredits_DisabledBrowser(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: false, RawCostUSDPerHour: 0.02, BillingIncrementSecs: 30}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q))
}

func TestEstimateBrowserIncrementCredits_ZeroRate(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 0, BillingIncrementSecs: 30}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q))
}

func TestEstimateBrowserIncrementCredits_30SecBucket(t *testing.T) {
	// $0.02/hour, 30s increment, markup 50%, 1000 c/$
	// raw per increment = 0.02 × (30/3600) = 0.000166... USD = 166.7 micro-USD
	// loaded = 166.7 × 1.5 = 250 micro-USD
	// credits = ceil(0.00025 × 1000) = ceil(0.25) = 1
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 0.02, BillingIncrementSecs: 30}}
	q := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	// minimum 1
	assert.GreaterOrEqual(t, EstimateBrowserIncrementCredits(res, q), int64(1))
}

func TestEstimateBrowserIncrementCredits_60SecBucket(t *testing.T) {
	// Use $3.6/hr so 30s = exactly $0.03 and 60s = exactly $0.06, eliminating rounding skew.
	res30 := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 3.6, BillingIncrementSecs: 30}}
	res60 := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 3.6, BillingIncrementSecs: 60}}
	q := domain.QuotaDefaults{ServiceMarkup: 0, CreditsPerUSD: 1000}
	c30 := EstimateBrowserIncrementCredits(res30, q)
	c60 := EstimateBrowserIncrementCredits(res60, q)
	assert.Equal(t, c30*2, c60, "60s bucket should be exactly 2× the 30s bucket")
}

func TestEstimateBrowserIncrementCredits_DefaultBucketWhenZero(t *testing.T) {
	// BillingIncrementSecs == 0 should default to 30s internally
	res0 := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 1.0, BillingIncrementSecs: 0}}
	res30 := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 1.0, BillingIncrementSecs: 30}}
	q := domain.QuotaDefaults{ServiceMarkup: 0, CreditsPerUSD: 1000}
	assert.Equal(t, EstimateBrowserIncrementCredits(res30, q), EstimateBrowserIncrementCredits(res0, q))
}

func TestLoadedCostMicro_Basic(t *testing.T) {
	// raw 2000 micro-USD, markup 50% → 3000 micro-USD
	assert.Equal(t, int64(3000), LoadedCostMicro(2000, 0.5))
}

func TestLoadedCostMicro_ZeroMarkup(t *testing.T) {
	assert.Equal(t, int64(2000), LoadedCostMicro(2000, 0))
}

func TestLoadedCostMicro_ZeroInput(t *testing.T) {
	assert.Equal(t, int64(0), LoadedCostMicro(0, 0.5))
}
