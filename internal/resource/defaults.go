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
		Enabled:              false,
		RawCostUSDPerHour:    0.02,
		BillingIncrementSecs: 30,
	},
	Render: domain.RenderMeteringConfig{
		Enabled:             false,
		RawCostUSDPerRender: 0.002,
	},
}

type settingsKV interface {
	Get(userID, key string, out any) error
	Set(userID, key string, v any) error
}

// LoadDefaults returns the merged resource metering configuration.
// Stored values win for boolean fields (unlike numeric fields which only override when non-zero,
// booleans are always stored as-is to allow explicit Enabled:false).
func LoadDefaults(store settingsKV) domain.ResourceMeteringDefaults {
	var stored domain.ResourceMeteringDefaults
	if err := store.Get(domain.SystemUserID, defaultsKey, &stored); err != nil {
		return builtinDefaults
	}
	// For resource metering we store the full struct and use it directly — no partial merge.
	// The only fixup: enforce a safe BillingIncrementSecs range if someone set a bad value.
	if stored.Browser.BillingIncrementSecs < 5 || stored.Browser.BillingIncrementSecs > 300 {
		stored.Browser.BillingIncrementSecs = builtinDefaults.Browser.BillingIncrementSecs
	}
	return stored
}

// SaveDefaults validates then persists resource metering configuration.
func SaveDefaults(store settingsKV, d domain.ResourceMeteringDefaults) error {
	if err := validateDefaults(d); err != nil {
		return err
	}
	return store.Set(domain.SystemUserID, defaultsKey, d)
}

// validateDefaults returns an error if any field is out of acceptable range.
func validateDefaults(d domain.ResourceMeteringDefaults) error {
	if d.Browser.RawCostUSDPerHour < 0 {
		return errors.New("resource_metering: browser raw_cost_usd_per_hour must be >= 0")
	}
	if d.Browser.BillingIncrementSecs != 0 {
		if d.Browser.BillingIncrementSecs < 5 || d.Browser.BillingIncrementSecs > 300 {
			return errors.New("resource_metering: browser billing_increment_seconds must be 5–300")
		}
	}
	if d.Render.RawCostUSDPerRender < 0 {
		return errors.New("resource_metering: render raw_cost_usd_per_render must be >= 0")
	}
	return nil
}
