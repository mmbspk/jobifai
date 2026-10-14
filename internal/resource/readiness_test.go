package resource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resource"
)

// quotaDefaults returns economics that produce non-zero credits at the given
// raw rates: CreditsPerUSD=1000, ServiceMarkup=0 (to simplify assertions).
func testQD() domain.QuotaDefaults {
	return domain.QuotaDefaults{
		CreditsPerUSD: 1000,
		ServiceMarkup: 0,
	}
}

// cfgBoth returns a config with master enabled, both resource types enabled,
// and rates generous enough to produce ≥1 credit per event.
// Browser: 3 600 000 µ-USD/hr, increment=30s → raw/inc = 30 000 µ-USD
//   → 30 000 × 1.0 × 1000 / 1_000_000 = 30 credits/increment.
// Render:  1 000 000 µ-USD/render → 1000 credits/render.
func cfgBoth() domain.ResourceMeteringDefaults {
	return domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{
			Enabled:                true,
			RawCostUSDMicroPerHour: 3_600_000,
			BillingIncrementSecs:   30,
		},
		Render: domain.RenderMeteringConfig{
			Enabled:                  true,
			RawCostUSDMicroPerRender: 1_000_000,
		},
	}
}

// cfgMasterDisabled has everything set but master toggled off.
func cfgMasterDisabled() domain.ResourceMeteringDefaults {
	c := cfgBoth()
	c.Enabled = false
	return c
}

// cfgBrowserZeroRate has browser enabled but zero raw cost → 0 credits per increment.
func cfgBrowserZeroRate() domain.ResourceMeteringDefaults {
	c := cfgBoth()
	c.Browser.RawCostUSDMicroPerHour = 0
	return c
}

// cfgRenderZeroRate has render enabled but zero raw cost.
func cfgRenderZeroRate() domain.ResourceMeteringDefaults {
	c := cfgBoth()
	c.Render.RawCostUSDMicroPerRender = 0
	return c
}

func TestComputeReadiness_Unreviewed(t *testing.T) {
	r := resource.ComputeReadiness(domain.ResourceMeteringDefaults{}, false, testQD())
	assert.False(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusUnreviewed, r.Status)
	assert.False(t, r.DefaultsPersisted)
	assert.NotEmpty(t, r.Warnings)
}

func TestComputeReadiness_MasterDisabled_Persisted(t *testing.T) {
	r := resource.ComputeReadiness(cfgMasterDisabled(), true, testQD())
	assert.True(t, r.Ready, "explicitly disabled metering should be ready=true")
	assert.Equal(t, domain.ResourceMeteringStatusDisabled, r.Status)
	assert.True(t, r.DefaultsPersisted)
	assert.Empty(t, r.Warnings)
}

func TestComputeReadiness_ReadyBothEnabled(t *testing.T) {
	r := resource.ComputeReadiness(cfgBoth(), true, testQD())
	assert.True(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusReady, r.Status)
	assert.True(t, r.BrowserAutomation.Ready)
	assert.True(t, r.DocumentRender.Ready)
	assert.Empty(t, r.Warnings)
}

func TestComputeReadiness_BrowserZeroRate_Invalid(t *testing.T) {
	r := resource.ComputeReadiness(cfgBrowserZeroRate(), true, testQD())
	assert.False(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusInvalid, r.Status)
	assert.False(t, r.BrowserAutomation.Ready)
	assert.True(t, r.DocumentRender.Ready)
	require.NotEmpty(t, r.Warnings)
	assert.Contains(t, r.Warnings[0], "Browser automation")
}

func TestComputeReadiness_RenderZeroRate_Invalid(t *testing.T) {
	r := resource.ComputeReadiness(cfgRenderZeroRate(), true, testQD())
	assert.False(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusInvalid, r.Status)
	assert.True(t, r.BrowserAutomation.Ready)
	assert.False(t, r.DocumentRender.Ready)
	require.NotEmpty(t, r.Warnings)
}

func TestComputeReadiness_BothZeroRate_Invalid(t *testing.T) {
	cfg := cfgBoth()
	cfg.Browser.RawCostUSDMicroPerHour = 0
	cfg.Render.RawCostUSDMicroPerRender = 0
	r := resource.ComputeReadiness(cfg, true, testQD())
	assert.False(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusInvalid, r.Status)
	assert.Len(t, r.Warnings, 2)
}

func TestComputeReadiness_BrowserDisabled_RenderValid_Ready(t *testing.T) {
	cfg := cfgBoth()
	cfg.Browser.Enabled = false
	r := resource.ComputeReadiness(cfg, true, testQD())
	assert.True(t, r.Ready, "disabled resource type must not fail overall readiness")
	assert.Equal(t, domain.ResourceMeteringStatusReady, r.Status)
	assert.True(t, r.BrowserAutomation.Ready, "explicitly disabled browser → ready=true")
}

func TestComputeReadiness_RenderDisabled_BrowserValid_Ready(t *testing.T) {
	cfg := cfgBoth()
	cfg.Render.Enabled = false
	r := resource.ComputeReadiness(cfg, true, testQD())
	assert.True(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusReady, r.Status)
}

func TestComputeReadiness_CreditsPerUSDZero_Invalid(t *testing.T) {
	qd := testQD()
	qd.CreditsPerUSD = 0 // pricing pipeline returns 0
	r := resource.ComputeReadiness(cfgBoth(), true, qd)
	assert.False(t, r.Ready)
	assert.Equal(t, domain.ResourceMeteringStatusInvalid, r.Status)
}

func TestComputeReadiness_BrowserCreditsPerMinute(t *testing.T) {
	// raw=3600000 µ-USD/hr, inc=30s → per-inc raw = 3600000×30/3600 = 30000
	// credits = 30000 × 1000 / 1_000_000 = 30
	// credits/min = 30 × (60/30) = 60.0
	r := resource.ComputeReadiness(cfgBoth(), true, testQD())
	assert.InDelta(t, 60.0, r.BrowserAutomation.CreditsPerMinute, 0.01)
}

func TestComputeReadiness_RenderCreditsPerRender(t *testing.T) {
	// raw=1000000 µ-USD, CreditsPerUSD=1000 → 1000×1000/1000000 = 1
	r := resource.ComputeReadiness(cfgBoth(), true, testQD())
	assert.Equal(t, int64(1000), r.DocumentRender.CreditsPerRender)
}

func TestComputeReadiness_WarningsAlwaysSlice(t *testing.T) {
	// Warnings must never be nil — it must be a non-nil (possibly empty) slice so JSON serialises to [].
	r := resource.ComputeReadiness(cfgBoth(), true, testQD())
	require.NotNil(t, r.Warnings)
}

func TestComputeReadiness_UnreviewedIncludesResourceDetails(t *testing.T) {
	// Even when not reviewed, per-resource readiness must be populated (for display).
	r := resource.ComputeReadiness(cfgBoth(), false, testQD())
	// cfgBoth uses valid rates; sub-types should be ready (they inherit the good config).
	assert.True(t, r.BrowserAutomation.Ready)
	assert.True(t, r.DocumentRender.Ready)
}
