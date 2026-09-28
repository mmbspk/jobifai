package handler

import (
	"net/http"
	"strings"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
	"github.com/user/jobifai/internal/domain"
)

type publicPlan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Credits     int    `json:"credits"`
	TrialDays   int    `json:"trial_days,omitempty"`
	UnitAmount  *int64 `json:"unit_amount,omitempty"`
	Currency    string `json:"currency,omitempty"`
	Interval    string `json:"interval,omitempty"`
	Configured  bool   `json:"configured"`
}

type publicPlansResponse struct {
	Plans []publicPlan `json:"plans"`
}

func (h *BillingHandlers) PublicPlans(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "plans not configured"})
		return
	}

	def := h.svc.Quota.LoadDefaults()
	plans := []publicPlan{
		{
			ID:         domain.QuotaPlanTrial,
			Name:       "Trial",
			Credits:    def.TrialCredits,
			TrialDays:  def.TrialDays,
			UnitAmount: int64Ptr(0),
			Configured: true,
		},
		buildPaidPublicPlan(domain.QuotaPlanStarter, "Starter", def.StarterCreditsMonthly, def.StripePriceStarter),
		buildPaidPublicPlan(domain.QuotaPlanPro, "Pro", def.ProCreditsMonthly, def.StripePricePro),
	}

	writeJSON(w, http.StatusOK, publicPlansResponse{Plans: plans})
}

func buildPaidPublicPlan(id, name string, credits int, stripePriceID string) publicPlan {
	out := publicPlan{
		ID:         id,
		Name:       name,
		Credits:    credits,
		Configured: stripePriceID != "",
	}
	if stripePriceID == "" || !stripeEnabled() {
		return out
	}

	initStripeKey()
	p, err := price.Get(stripePriceID, &stripe.PriceParams{})
	if err != nil || p == nil {
		return out
	}
	out.UnitAmount = &p.UnitAmount
	out.Currency = strings.ToUpper(string(p.Currency))
	if p.Recurring != nil {
		out.Interval = string(p.Recurring.Interval)
	}
	return out
}

func int64Ptr(v int64) *int64 { return &v }
