package quota

import (
	"github.com/user/jobifai/internal/domain"
)

const keyQuotaDefaults = "quota_defaults"
const keyQuotaOverrides = "quota_overrides"

var builtinDefaults = domain.QuotaDefaults{
	EnforcementDefault:     true,
	CreditsPerUSD:          1000,
	ServiceMarkup:          0.5,
	PerCallFeeUSD:          0.002,
	TrialCredits:           500,
	TrialDays:              7,
	StarterCreditsMonthly:  3000,
	ProCreditsMonthly:      8000,
	SubscriberGraceCredits: 200,
	TopUpPacks: []domain.TopUpPack{
		{Credits: 1000, Label: "1,000 credits"},
		{Credits: 2500, Label: "2,500 credits"},
		{Credits: 5000, Label: "5,000 credits"},
	},
}

type settingsKV interface {
	Get(userID, key string, dst any) error
	Set(userID, key string, src any) error
}

func loadDefaults(store settingsKV) domain.QuotaDefaults {
	var d domain.QuotaDefaults
	if err := store.Get(domain.SystemUserID, keyQuotaDefaults, &d); err != nil {
		return builtinDefaults
	}
	out := builtinDefaults
	if d.CreditsPerUSD > 0 {
		out.CreditsPerUSD = d.CreditsPerUSD
	}
	if d.ServiceMarkup >= 0 {
		out.ServiceMarkup = d.ServiceMarkup
	}
	if d.PerCallFeeUSD >= 0 {
		out.PerCallFeeUSD = d.PerCallFeeUSD
	}
	if d.TrialCredits > 0 {
		out.TrialCredits = d.TrialCredits
	}
	if d.TrialDays > 0 {
		out.TrialDays = d.TrialDays
	}
	if d.StarterCreditsMonthly > 0 {
		out.StarterCreditsMonthly = d.StarterCreditsMonthly
	}
	if d.ProCreditsMonthly > 0 {
		out.ProCreditsMonthly = d.ProCreditsMonthly
	}
	if d.SubscriberGraceCredits > 0 {
		out.SubscriberGraceCredits = d.SubscriberGraceCredits
	}
	out.EnforcementDefault = d.EnforcementDefault
	if d.StripePriceStarter != "" {
		out.StripePriceStarter = d.StripePriceStarter
	}
	if d.StripePricePro != "" {
		out.StripePricePro = d.StripePricePro
	}
	if len(d.TopUpPacks) > 0 {
		out.TopUpPacks = d.TopUpPacks
	}
	return out
}

func saveDefaults(store settingsKV, d domain.QuotaDefaults) error {
	return store.Set(domain.SystemUserID, keyQuotaDefaults, d)
}

func loadOverrides(store settingsKV, userID string) domain.QuotaUserOverrides {
	var o domain.QuotaUserOverrides
	_ = store.Get(userID, keyQuotaOverrides, &o)
	return o
}

func saveOverrides(store settingsKV, userID string, o domain.QuotaUserOverrides) error {
	return store.Set(userID, keyQuotaOverrides, o)
}

// AllowanceCreditsForPlan returns monthly credit grant for starter/pro.
func AllowanceCreditsForPlan(def domain.QuotaDefaults, plan string) int64 {
	switch plan {
	case domain.QuotaPlanStarter:
		return int64(def.StarterCreditsMonthly)
	case domain.QuotaPlanPro:
		return int64(def.ProCreditsMonthly)
	default:
		return 0
	}
}

const KeyQuotaDefaults = keyQuotaDefaults
const KeyQuotaOverrides = keyQuotaOverrides
