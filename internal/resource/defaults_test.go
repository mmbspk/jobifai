package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// fakeKV is an in-memory settingsKV for tests.
type fakeKV struct {
	data map[string]domain.ResourceMeteringDefaults
}

func newFakeKV() *fakeKV { return &fakeKV{data: make(map[string]domain.ResourceMeteringDefaults)} }

func (f *fakeKV) Get(userID, key string, out any) error {
	v, ok := f.data[userID+"/"+key]
	if !ok {
		return errNotFound
	}
	*(out.(*domain.ResourceMeteringDefaults)) = v
	return nil
}

func (f *fakeKV) Set(userID, key string, v any) error {
	f.data[userID+"/"+key] = v.(domain.ResourceMeteringDefaults)
	return nil
}

var errNotFound = &notFoundErr{}

type notFoundErr struct{}

func (e *notFoundErr) Error() string { return "not found" }

func TestLoadDefaults_NoStoredConfig_ReturnsBuiltin(t *testing.T) {
	kv := newFakeKV()
	got := LoadDefaults(kv)
	assert.Equal(t, builtinDefaults.Enabled, got.Enabled)
	assert.Equal(t, builtinDefaults.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
}

func TestSaveAndLoadDefaults_RoundTrip(t *testing.T) {
	kv := newFakeKV()
	d := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 0.05, BillingIncrementSecs: 60},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDPerRender: 0.003},
	}
	require.NoError(t, SaveDefaults(kv, d))
	got := LoadDefaults(kv)
	assert.Equal(t, d.Enabled, got.Enabled)
	assert.Equal(t, d.Browser.Enabled, got.Browser.Enabled)
	assert.InDelta(t, d.Browser.RawCostUSDPerHour, got.Browser.RawCostUSDPerHour, 0.0001)
	assert.Equal(t, d.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
	assert.InDelta(t, d.Render.RawCostUSDPerRender, got.Render.RawCostUSDPerRender, 0.0001)
}

func TestSaveAndLoadDefaults_ExplicitDisabledPreserved(t *testing.T) {
	kv := newFakeKV()
	d := domain.ResourceMeteringDefaults{Enabled: false, Browser: domain.BrowserMeteringConfig{Enabled: false, BillingIncrementSecs: 30}}
	require.NoError(t, SaveDefaults(kv, d))
	got := LoadDefaults(kv)
	assert.False(t, got.Enabled, "Enabled:false must not be overridden by builtin")
	assert.False(t, got.Browser.Enabled)
}

func TestValidateDefaults_NegativeBrowserRate(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{RawCostUSDPerHour: -0.01, BillingIncrementSecs: 30},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw_cost_usd_per_hour")
}

func TestValidateDefaults_BillingIncrementTooSmall(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 4},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "billing_increment_seconds")
}

func TestValidateDefaults_BillingIncrementTooLarge(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 301},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "billing_increment_seconds")
}

func TestValidateDefaults_ZeroIncrementAllowed(t *testing.T) {
	// BillingIncrementSecs == 0 is allowed at save time (treated as "use default 30")
	d := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 0},
	}
	require.NoError(t, SaveDefaults(newFakeKV(), d))
}

func TestValidateDefaults_NegativeRenderRate(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Render: domain.RenderMeteringConfig{RawCostUSDPerRender: -0.01},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw_cost_usd_per_render")
}

func TestValidateDefaults_AllValid(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDPerHour: 0.02, BillingIncrementSecs: 30},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDPerRender: 0.002},
	}
	require.NoError(t, SaveDefaults(newFakeKV(), d))
}

func TestLoadDefaults_BadIncrementInStore_FixedToBuiltin(t *testing.T) {
	// If a bad value somehow slipped into the store, LoadDefaults corrects it.
	kv := newFakeKV()
	bad := domain.ResourceMeteringDefaults{Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 999}}
	kv.data[domain.SystemUserID+"/"+defaultsKey] = bad
	got := LoadDefaults(kv)
	assert.Equal(t, builtinDefaults.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
}
