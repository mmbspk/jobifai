package resource

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// fakeKV is an in-memory settingsKV for tests.
type fakeKV struct {
	data    map[string]domain.ResourceMeteringDefaults
	failGet bool // when true, Get returns a non-not-found error (simulates infra failure)
}

func newFakeKV() *fakeKV { return &fakeKV{data: make(map[string]domain.ResourceMeteringDefaults)} }

func (f *fakeKV) Get(userID, key string, out any) error {
	if f.failGet {
		return errors.New("simulated storage failure")
	}
	v, ok := f.data[userID+"/"+key]
	if !ok {
		return domain.ErrNotFound
	}
	*(out.(*domain.ResourceMeteringDefaults)) = v
	return nil
}

func (f *fakeKV) Set(userID, key string, v any) error {
	f.data[userID+"/"+key] = v.(domain.ResourceMeteringDefaults)
	return nil
}

func TestLoadDefaults_NoStoredConfig_ReturnsBuiltin(t *testing.T) {
	kv := newFakeKV()
	got, err := LoadDefaults(kv)
	require.NoError(t, err)
	assert.Equal(t, builtinDefaults.Enabled, got.Enabled)
	assert.Equal(t, builtinDefaults.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
}

func TestLoadDefaults_StorageError_ReturnsError(t *testing.T) {
	kv := &fakeKV{data: make(map[string]domain.ResourceMeteringDefaults), failGet: true}
	_, err := LoadDefaults(kv)
	require.Error(t, err, "non-not-found storage error must propagate (fail-closed)")
	assert.False(t, errors.Is(err, domain.ErrNotFound), "error must not be ErrNotFound")
}

func TestSaveAndLoadDefaults_RoundTrip(t *testing.T) {
	kv := newFakeKV()
	d := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 50_000, BillingIncrementSecs: 60},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 3_000},
	}
	require.NoError(t, SaveDefaults(kv, d))
	got, err := LoadDefaults(kv)
	require.NoError(t, err)
	assert.Equal(t, d.Enabled, got.Enabled)
	assert.Equal(t, d.Browser.Enabled, got.Browser.Enabled)
	assert.Equal(t, d.Browser.RawCostUSDMicroPerHour, got.Browser.RawCostUSDMicroPerHour)
	assert.Equal(t, d.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
	assert.Equal(t, d.Render.RawCostUSDMicroPerRender, got.Render.RawCostUSDMicroPerRender)
}

func TestSaveAndLoadDefaults_ExplicitDisabledPreserved(t *testing.T) {
	kv := newFakeKV()
	d := domain.ResourceMeteringDefaults{Enabled: false, Browser: domain.BrowserMeteringConfig{Enabled: false, BillingIncrementSecs: 30}}
	require.NoError(t, SaveDefaults(kv, d))
	got, err := LoadDefaults(kv)
	require.NoError(t, err)
	assert.False(t, got.Enabled, "Enabled:false must not be overridden by builtin")
	assert.False(t, got.Browser.Enabled)
}

func TestValidateDefaults_NegativeBrowserRate(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{RawCostUSDMicroPerHour: -1, BillingIncrementSecs: 30},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw_cost_usd_micro_per_hour")
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
		Render: domain.RenderMeteringConfig{RawCostUSDMicroPerRender: -1},
	}
	err := SaveDefaults(newFakeKV(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw_cost_usd_micro_per_render")
}

func TestValidateDefaults_AllValid(t *testing.T) {
	d := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 20_000, BillingIncrementSecs: 30},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}
	require.NoError(t, SaveDefaults(newFakeKV(), d))
}

func TestLoadDefaults_BadIncrementInStore_FixedToBuiltin(t *testing.T) {
	// If a bad value somehow slipped into the store, LoadDefaults corrects it.
	kv := newFakeKV()
	bad := domain.ResourceMeteringDefaults{Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 999}}
	kv.data[domain.SystemUserID+"/"+defaultsKey] = bad
	got, err := LoadDefaults(kv)
	require.NoError(t, err)
	assert.Equal(t, builtinDefaults.Browser.BillingIncrementSecs, got.Browser.BillingIncrementSecs)
}
