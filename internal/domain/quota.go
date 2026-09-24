package domain

import "time"

// Quota plan identifiers stored on user_quota.plan.
const (
	QuotaPlanTrial   = "trial"
	QuotaPlanStarter = "starter"
	QuotaPlanPro     = "pro"
)

// TopUpPack defines a one-time credit pack (Stripe Price + grant size).
type TopUpPack struct {
	Credits int64  `json:"credits"`
	PriceID string `json:"stripe_price_id,omitempty"`
	Label   string `json:"label,omitempty"`
}

// QuotaDefaults are deployment-wide tunables (settings key quota_defaults on SystemUserID).
type QuotaDefaults struct {
	EnforcementDefault bool `json:"enforcement_default"`

	// CreditsPerUSD: loaded burn credits per 1 USD (e.g. 1000 => 1000 credits ≈ $1 loaded cost).
	CreditsPerUSD float64 `json:"credits_per_usd"`
	// ServiceMarkup is added on top of LLM USD (0.5 = +50% service/infra).
	ServiceMarkup float64 `json:"service_markup"`
	// PerCallFeeUSD is a flat USD fee added to every LLM call before converting to credits.
	PerCallFeeUSD float64 `json:"per_call_fee_usd"`

	TrialCredits int `json:"trial_credits"`
	TrialDays    int `json:"trial_days"`

	StarterCreditsMonthly int `json:"starter_credits_monthly"`
	ProCreditsMonthly     int `json:"pro_credits_monthly"`

	// SubscriberGraceCredits: max extra credits allowed during one automation session past the plan bucket.
	SubscriberGraceCredits int64 `json:"subscriber_grace_credits"`

	StripePriceStarter string `json:"stripe_price_starter,omitempty"`
	StripePricePro     string `json:"stripe_price_pro,omitempty"`
	TopUpPacks         []TopUpPack `json:"top_up_packs,omitempty"`
}

// QuotaUserOverrides optional per-user admin overrides (settings key quota_overrides).
type QuotaUserOverrides struct {
	EnforcementEnabled   *bool  `json:"enforcement_enabled,omitempty"`
	TrialCredits         *int   `json:"trial_credits,omitempty"`
	PeriodAllowanceCredits *int64 `json:"period_allowance_credits,omitempty"`
}

// QuotaStatus is returned to clients for UI and gating.
type QuotaStatus struct {
	EnforcementEnabled bool   `json:"enforcement_enabled"`
	Plan               string `json:"plan"`
	Unlimited          bool   `json:"unlimited"`

	UsedCredits      int64   `json:"used_credits"`
	AllowanceCredits int64   `json:"allowance_credits"`
	RemainingCredits int64   `json:"remaining_credits"`
	UsagePercent     float64 `json:"usage_percent"`

	TopUpCreditsRemaining int64 `json:"topup_credits_remaining"`

	PeriodStart *string `json:"period_start,omitempty"`
	PeriodEnd   *string `json:"period_end,omitempty"`
	TrialEndsAt *string `json:"trial_ends_at,omitempty"`

	TrialRemainingCredits *int64 `json:"trial_remaining_credits,omitempty"`
	OverageDebtCredits    int64  `json:"overage_debt_credits"`

	Blocked            bool `json:"blocked"`
	BlockCode          string `json:"block_code,omitempty"`
	GraceSessionActive bool   `json:"grace_session_active"`
	StripeConfigured   bool   `json:"stripe_configured"`

	TopUpPacks []TopUpPack `json:"top_up_packs,omitempty"`
}

// UserQuotaRow mirrors the user_quota table (legacy SQL column names *_micro store credits).
type UserQuotaRow struct {
	UserID               string
	Plan                 string
	EnforcementEnabled   bool
	TrialRemainingCredits int64
	TrialEndsAt          *time.Time
	PeriodAllowanceCredits int64
	PeriodUsedCredits    int64
	TopUpCreditsRemaining int64
	PeriodStart          *time.Time
	PeriodEnd            *time.Time
	OverageDebtCredits   int64
	StripeCustomerID     string
	StripeSubscriptionID string
}
