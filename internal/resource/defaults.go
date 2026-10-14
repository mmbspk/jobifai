package resource

import (
	"errors"

	"github.com/user/jobifai/internal/domain"
)

const defaultsKey = "resource_metering_defaults"

// builtinDefaults are conservative dev/test defaults.
// Both master and per-type Enabled are false so no credits are burned until an admin
// explicitly reviews and enables resource metering.
var builtinDefaults = domain.ResourceMeteringDefaults{
	Enabled: false,
	Browser: domain.BrowserMeteringConfig{
		Enabled:                false,
		RawCostUSDMicroPerHour: 20_000, // $0.02/hr = 20 000 micro-USD
		BillingIncrementSecs:   30,
	},
	Render: domain.RenderMeteringConfig{
		Enabled:                  false,
		RawCostUSDMicroPerRender: 2_000, // $0.002/render = 2 000 micro-USD
	},
}

type settingsKV interface {
	Get(userID, key string, out any) error
	Set(userID, key string, v any) error
}

// LoadDefaults returns the stored resource metering configuration.
// Fail-closed: returns an error for any storage failure other than "not found".
// A "not found" result falls back to builtinDefaults (safe: metering disabled).
func LoadDefaults(store settingsKV) (domain.ResourceMeteringDefaults, error) {
	var stored domain.ResourceMeteringDefaults
	err := store.Get(domain.SystemUserID, defaultsKey, &stored)
	if err == nil {
		if stored.Browser.BillingIncrementSecs < 5 || stored.Browser.BillingIncrementSecs > 300 {
			stored.Browser.BillingIncrementSecs = builtinDefaults.Browser.BillingIncrementSecs
		}
		return stored, nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		return builtinDefaults, nil
	}
	return domain.ResourceMeteringDefaults{}, err
}

// SaveDefaults validates then persists resource metering configuration.
func SaveDefaults(store settingsKV, d domain.ResourceMeteringDefaults) error {
	if err := validateDefaults(d); err != nil {
		return err
	}
	return store.Set(domain.SystemUserID, defaultsKey, d)
}

// DefaultsPersisted returns true when an admin has explicitly saved resource-metering
// configuration (i.e. the key exists in the config store).
// Returns (false, nil) when not yet persisted — the runtime is using built-in safe defaults.
// Returns (false, error) when the storage layer fails.
func DefaultsPersisted(store settingsKV) (bool, error) {
	var stored domain.ResourceMeteringDefaults
	err := store.Get(domain.SystemUserID, defaultsKey, &stored)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return false, err
}

// validateDefaults returns an error if any field is out of acceptable range.
func validateDefaults(d domain.ResourceMeteringDefaults) error {
	if d.Browser.RawCostUSDMicroPerHour < 0 {
		return errors.New("resource_metering: browser raw_cost_usd_micro_per_hour must be >= 0")
	}
	if d.Browser.BillingIncrementSecs != 0 {
		if d.Browser.BillingIncrementSecs < 5 || d.Browser.BillingIncrementSecs > 300 {
			return errors.New("resource_metering: browser billing_increment_seconds must be 5–300")
		}
	}
	if d.Render.RawCostUSDMicroPerRender < 0 {
		return errors.New("resource_metering: render raw_cost_usd_micro_per_render must be >= 0")
	}
	return nil
}
