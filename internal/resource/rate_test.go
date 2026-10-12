package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// helper to build a fully-enabled ResourceMeteringDefaults with one render config.
func enabledRender(rawMicroPerRender int64) domain.ResourceMeteringDefaults {
	return domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: rawMicroPerRender},
	}
}

// helper to build a fully-enabled ResourceMeteringDefaults with one browser config.
func enabledBrowser(rawMicroPerHour int64, incSecs int) domain.ResourceMeteringDefaults {
	return domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: rawMicroPerHour, BillingIncrementSecs: incSecs},
	}
}

var q1000 = domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}

// TestPriceResource_DisabledMaster verifies the master Enabled gate.
func TestPriceResource_DisabledMaster(t *testing.T) {
	res := domain.ResourceMeteringDefaults{
		Enabled: false,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}
	p := PriceResource(res, q1000, domain.ResourceTypeDocumentRender)
	assert.Equal(t, int64(0), p.Credits)
	assert.Equal(t, int64(0), p.RawCostUSDMicro)
}

// TestPriceResource_RenderDisabled verifies per-type Enabled gate.
func TestPriceResource_RenderDisabled(t *testing.T) {
	res := domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: false, RawCostUSDMicroPerRender: 2_000},
	}
	p := PriceResource(res, q1000, domain.ResourceTypeDocumentRender)
	assert.Equal(t, int64(0), p.Credits)
}

// TestPriceResource_RenderBasicCalculation: $0.002 render, 50% markup, 1000 c/$ = 3 credits.
func TestPriceResource_RenderBasicCalculation(t *testing.T) {
	// raw 2000 micro-USD × 1.5 = 3000 loaded → ceil(3000 × 1000 / 1_000_000) = ceil(3) = 3
	p := PriceResource(enabledRender(2_000), q1000, domain.ResourceTypeDocumentRender)
	assert.Equal(t, int64(2_000), p.RawCostUSDMicro)
	assert.Equal(t, int64(3_000), p.LoadedCostUSDMicro)
	assert.Equal(t, int64(3), p.Credits)
}

// TestPriceResource_RenderPerCallFeeNotApplied documents the intentional omission.
func TestPriceResource_RenderPerCallFeeNotApplied(t *testing.T) {
	qWithFee := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000, PerCallFeeUSD: 1.0}
	p := PriceResource(enabledRender(2_000), qWithFee, domain.ResourceTypeDocumentRender)
	assert.Equal(t, int64(3), p.Credits, "PerCallFeeUSD must not be added to resource pricing")
}

// TestPriceResource_RenderZeroRate verifies zero rate returns zero pricing.
func TestPriceResource_RenderZeroRate(t *testing.T) {
	p := PriceResource(enabledRender(0), q1000, domain.ResourceTypeDocumentRender)
	assert.Equal(t, int64(0), p.Credits)
}

// TestPriceResource_BrowserBasic verifies 30s increment minimum credit.
func TestPriceResource_BrowserBasic(t *testing.T) {
	// 20000 micro-USD/hr × 30s / 3600 = 166 micro-USD raw (integer)
	// loaded = ceil(166 × 1.5) = ceil(249) = 249 micro-USD
	// credits = ceil(249 × 1000 / 1_000_000) = ceil(0.249) = 1
	p := PriceResource(enabledBrowser(20_000, 30), q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, int64(166), p.RawCostUSDMicro, "integer arithmetic: 20000×30/3600=166")
	assert.GreaterOrEqual(t, p.Credits, int64(1), "minimum 1 credit when meter is enabled")
}

// TestPriceResource_BrowserIntegerArithmetic verifies deterministic truncation (F2).
// Float arithmetic would give 20000×30/3600 = 166.666... → rounds to 167.
// Integer arithmetic truncates: 20000×30 = 600_000; 600_000/3600 = 166.
func TestPriceResource_BrowserIntegerArithmetic(t *testing.T) {
	p := PriceResource(enabledBrowser(20_000, 30), q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, int64(166), p.RawCostUSDMicro,
		"integer division must truncate (not round); 20000×30/3600 = 166 not 167")
}

// TestPriceResource_BrowserAwkwardRate verifies that a rate which would produce
// sub-micro-USD fractions with float arithmetic gives stable zero rawMicro (F2).
func TestPriceResource_BrowserAwkwardRate(t *testing.T) {
	// 1 micro-USD/hr × 1s / 3600 = 0 (integer truncation)
	// With float: 1 × 1 / 3600 = 0.000277... → would round to something non-zero
	p := PriceResource(enabledBrowser(1, 1), q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, int64(0), p.Credits,
		"1 micro-USD/hr × 1s results in 0 raw micro-USD (integer truncation) → no charge")
}

// TestPriceResource_BrowserDoubleIncrement verifies proportional cost scaling.
func TestPriceResource_BrowserDoubleIncrement(t *testing.T) {
	// Use a rate that divides evenly at both 30s and 60s.
	// 3_600_000 micro-USD/hr × 30s / 3600 = 30_000 micro-USD raw
	// 3_600_000 micro-USD/hr × 60s / 3600 = 60_000 micro-USD raw
	p30 := PriceResource(enabledBrowser(3_600_000, 30), q1000, domain.ResourceTypeBrowserAutomation)
	p60 := PriceResource(enabledBrowser(3_600_000, 60), q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, p30.Credits*2, p60.Credits, "60s bucket must be 2× the 30s bucket")
}

// TestPriceResource_BrowserDefaultBucketWhenZero verifies BillingIncrementSecs=0 → 30s.
func TestPriceResource_BrowserDefaultBucketWhenZero(t *testing.T) {
	p0 := PriceResource(enabledBrowser(20_000, 0), q1000, domain.ResourceTypeBrowserAutomation)
	p30 := PriceResource(enabledBrowser(20_000, 30), q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, p30.Credits, p0.Credits, "zero BillingIncrementSecs must default to 30")
}

// TestPriceResource_UnknownType verifies unknown resource types return zero.
func TestPriceResource_UnknownType(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true}
	p := PriceResource(res, q1000, domain.ResourceType("unknown_resource"))
	assert.Equal(t, int64(0), p.Credits)
}

// TestEstimateRenderCredits_DisabledMaster delegates to PriceResource.
func TestEstimateRenderCredits_DisabledMaster(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: false, Render: domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000}}
	assert.Equal(t, int64(0), EstimateRenderCredits(res, q1000))
}

// TestEstimateRenderCredits_DisabledRender verifies per-type gate.
func TestEstimateRenderCredits_DisabledRender(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Render: domain.RenderMeteringConfig{Enabled: false, RawCostUSDMicroPerRender: 2_000}}
	assert.Equal(t, int64(0), EstimateRenderCredits(res, q1000))
}

// TestEstimateRenderCredits_Calculation verifies correct credits for known rates.
func TestEstimateRenderCredits_Calculation(t *testing.T) {
	// 2000 micro-USD × 1.5 markup × 1000 c/$ / 1_000_000 = 3 credits
	res := domain.ResourceMeteringDefaults{Enabled: true, Render: domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000}}
	assert.Equal(t, int64(3), EstimateRenderCredits(res, q1000))
}

// TestEstimateBrowserIncrementCredits_DisabledMaster verifies master gate.
func TestEstimateBrowserIncrementCredits_DisabledMaster(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: false, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 20_000, BillingIncrementSecs: 30}}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q1000))
}

// TestEstimateBrowserIncrementCredits_DisabledBrowser verifies per-type gate.
func TestEstimateBrowserIncrementCredits_DisabledBrowser(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: false, RawCostUSDMicroPerHour: 20_000, BillingIncrementSecs: 30}}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q1000))
}

// TestEstimateBrowserIncrementCredits_ZeroRate verifies zero rate returns zero.
func TestEstimateBrowserIncrementCredits_ZeroRate(t *testing.T) {
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 0, BillingIncrementSecs: 30}}
	assert.Equal(t, int64(0), EstimateBrowserIncrementCredits(res, q1000))
}

// TestEstimateBrowserIncrementCredits_ExactArithmetic proves that integer arithmetic is
// used and produces the documented deterministic result (F2).
func TestEstimateBrowserIncrementCredits_ExactArithmetic(t *testing.T) {
	// 20000 micro-USD/hr × 30s / 3600 = 166 (integer)  ≠ 167 (round from 166.66...)
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 20_000, BillingIncrementSecs: 30}}
	p := PriceResource(res, q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, int64(166), p.RawCostUSDMicro,
		"F2: integer truncation gives 20000×30/3600=166, not rounded 167")
}

// TestPriceResource_CreditsParityWithAIBilling proves that for the same loaded micro-USD,
// resource billing produces the same credit count as quota.CreditsFromLoadedUSD (the AI path).
// This verifies the unified conversion model — one rounding rule for all billing.
func TestPriceResource_CreditsParityWithAIBilling(t *testing.T) {
	qd := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	cases := []struct {
		name    string
		rawMicro int64
	}{
		{"exact 3 credits", 2_000},       // loaded=3000 → credits=3
		{"minimum 1 credit", 166},        // loaded=249 → credits=0 → min 1
		{"large render", 10_000},         // loaded=15_000 → credits=15
		{"awkward: 3.3 credits", 2_200},  // loaded=3300 → credits=3 (truncate, not ceil)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := enabledRender(tc.rawMicro)
			p := PriceResource(res, qd, domain.ResourceTypeDocumentRender)
			// Compute loaded micro-USD the same way pricingFromRaw does.
			markup := qd.ServiceMarkup
			loadedMicro := int64(float64(tc.rawMicro) / 1_000_000 * (1 + markup) * 1_000_000)
			if loadedMicro <= 0 {
				loadedMicro = tc.rawMicro
			}
			want := quota.CreditsFromLoadedUSD(qd, loadedMicro)
			assert.Equal(t, want, p.Credits,
				"resource credits must equal quota.CreditsFromLoadedUSD for same loaded cost")
		})
	}
}

// TestPriceResource_AwkwardRateCreditsMatchAI proves a non-round rate doesn't diverge
// from the AI-billing conversion path (truncation in both, no separate rounding rules).
func TestPriceResource_AwkwardRateCreditsMatchAI(t *testing.T) {
	qd := domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}
	// 2201 micro-USD raw × 1.5 = 3301.5 loaded → loadedMicro=3301 (float64 truncation)
	// CreditsFromLoadedUSD(3301): loadedUSD=0.003301, credits=int64(0.003301*1000)=3
	rawMicro := int64(2_201)
	res := enabledRender(rawMicro)
	p := PriceResource(res, qd, domain.ResourceTypeDocumentRender)

	loadedMicro := int64(float64(rawMicro) / 1_000_000 * 1.5 * 1_000_000)
	want := quota.CreditsFromLoadedUSD(qd, loadedMicro)
	assert.Equal(t, want, p.Credits, "awkward rate must use same truncation as AI billing")
	assert.Equal(t, loadedMicro, p.LoadedCostUSDMicro, "loaded micro must match parity derivation")
}

// TestPriceResource_LoadedMicroMatchesQuotaPrimitive verifies that pricingFromRaw uses the
// shared quota.LoadedMicroFromRaw primitive and not a separate float pipeline.
func TestPriceResource_LoadedMicroMatchesQuotaPrimitive(t *testing.T) {
	cases := []struct {
		name     string
		rawMicro int64
		qd       domain.QuotaDefaults
	}{
		{"exact 3 credits", 2_000, domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}},
		{"awkward 2201", 2_201, domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}},
		{"tiny 7", 7, domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}},
		{"no markup", 999_999, domain.QuotaDefaults{ServiceMarkup: 0, CreditsPerUSD: 1000}},
		{"large 1500000", 1_500_000, domain.QuotaDefaults{ServiceMarkup: 0.5, CreditsPerUSD: 1000}},
		{"different markup 1.25", 2_000, domain.QuotaDefaults{ServiceMarkup: 1.25, CreditsPerUSD: 500}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := domain.ResourceMeteringDefaults{
				Enabled: true,
				Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: tc.rawMicro},
			}
			p := PriceResource(res, tc.qd, domain.ResourceTypeDocumentRender)
			wantLoaded := quota.LoadedMicroFromRaw(tc.qd, tc.rawMicro)
			wantCredits := quota.CreditsFromLoadedUSD(tc.qd, wantLoaded)
			assert.Equal(t, wantLoaded, p.LoadedCostUSDMicro,
				"LoadedCostUSDMicro must equal quota.LoadedMicroFromRaw")
			assert.Equal(t, wantCredits, p.Credits,
				"Credits must equal quota.CreditsFromLoadedUSD(LoadedMicroFromRaw)")
		})
	}
}

// TestEstimateBrowserIncrementCredits_AwkwardRate proves that an uncommon rate that is not
// divisible by 3600 still produces a stable integer result (F2).
func TestEstimateBrowserIncrementCredits_AwkwardRate(t *testing.T) {
	// 7201 micro-USD/hr × 30s / 3600:
	//   integer: 7201×30 = 216030; 216030/3600 = 60
	//   float:   7201×30/3600 = 60.0083... → could round to 60 or 60+ depending on impl
	// The key property: the result is EXACTLY 60 with integer arithmetic.
	res := domain.ResourceMeteringDefaults{Enabled: true, Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 7201, BillingIncrementSecs: 30}}
	p := PriceResource(res, q1000, domain.ResourceTypeBrowserAutomation)
	assert.Equal(t, int64(60), p.RawCostUSDMicro,
		"F2: 7201×30/3600 must be exactly 60 (integer truncation, not float rounding)")
}
