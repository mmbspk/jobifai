package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stripe/stripe-go/v82"
	billingportalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/billing"
	"github.com/user/jobifai/internal/domain"
)

// BillingHandlers Stripe checkout + webhooks.
type BillingHandlers struct{ svc *Services }

func NewBillingHandlers(svc *Services) *BillingHandlers { return &BillingHandlers{svc: svc} }

func stripeEnabled() bool {
	return billing.StripeConfigured()
}

func initStripeKey() {
	if k := billing.StripeSecretKey(); k != "" {
		stripe.Key = k
	}
}

// POST /api/billing/checkout  { "plan": "starter" | "pro" }
func (h *BillingHandlers) Checkout(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil || !stripeEnabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "billing not configured"})
		return
	}
	initStripeKey()
	userID := auth.UserIDFromCtx(r.Context())
	u, err := h.svc.Users.ByID(userID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "user not found"})
		return
	}
	var req struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	def := h.svc.Quota.LoadDefaults()
	priceID, plan := priceForPlan(def, req.Plan)
	if priceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unknown plan or Stripe price not configured"})
		return
	}
	customerID, err := h.ensureStripeCustomer(userID, u.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	baseURL := billing.AppBaseURL()
	sess, err := checkoutsession.New(&stripe.CheckoutSessionParams{
		Customer: stripe.String(customerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SuccessURL: stripe.String(baseURL + "/settings/plan?checkout=success"),
		CancelURL:  stripe.String(baseURL + "/settings/plan?checkout=cancel"),
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"jobifai_user_id": userID,
				"jobifai_plan":    plan,
			},
		},
		Metadata: map[string]string{
			"jobifai_user_id": userID,
			"jobifai_plan":    plan,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("stripe checkout session")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "checkout failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": sess.URL})
}

// POST /api/billing/topup  { "credits": 1000 }
func (h *BillingHandlers) TopUp(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil || !stripeEnabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "billing not configured"})
		return
	}
	initStripeKey()
	userID := auth.UserIDFromCtx(r.Context())
	u, err := h.svc.Users.ByID(userID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "user not found"})
		return
	}
	var req struct {
		Credits int64 `json:"credits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Credits <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "credits is required"})
		return
	}
	def := h.svc.Quota.LoadDefaults()
	priceID := topUpPriceID(def, req.Credits)
	if priceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "top-up pack not configured"})
		return
	}
	customerID, err := h.ensureStripeCustomer(userID, u.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	baseURL := billing.AppBaseURL()
	sess, err := checkoutsession.New(&stripe.CheckoutSessionParams{
		Customer: stripe.String(customerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SuccessURL: stripe.String(baseURL + "/settings/plan?topup=success"),
		CancelURL:  stripe.String(baseURL + "/settings/plan?topup=cancel"),
		Metadata: map[string]string{
			"jobifai_user_id":  userID,
			"jobifai_topup":    "1",
			"jobifai_credits": strconv.FormatInt(req.Credits, 10),
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("stripe topup checkout")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "checkout failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": sess.URL})
}

func topUpPriceID(def domain.QuotaDefaults, credits int64) string {
	for _, p := range def.TopUpPacks {
		if p.Credits == credits && p.PriceID != "" {
			return p.PriceID
		}
	}
	return ""
}

func (h *BillingHandlers) ensureStripeCustomer(userID, email string) (string, error) {
	row, err := h.svc.Quota.RowForUser(userID)
	if err != nil {
		return "", err
	}
	if row.StripeCustomerID != "" {
		return row.StripeCustomerID, nil
	}
	cust, err := customer.New(&stripe.CustomerParams{
		Email: stripe.String(email),
		Metadata: map[string]string{
			"jobifai_user_id": userID,
		},
	})
	if err != nil {
		return "", err
	}
	_ = h.svc.Quota.SetStripeCustomer(userID, cust.ID)
	return cust.ID, nil
}

// POST /api/billing/portal
func (h *BillingHandlers) Portal(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil || !stripeEnabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "billing not configured"})
		return
	}
	initStripeKey()
	userID := auth.UserIDFromCtx(r.Context())
	row, err := h.svc.Quota.RowForUser(userID)
	if err != nil || row.StripeCustomerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "no billing account yet"})
		return
	}
	ps, err := billingportalsession.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(row.StripeCustomerID),
		ReturnURL: stripe.String(billing.AppBaseURL() + "/settings/plan"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "portal failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": ps.URL})
}

// POST /api/billing/webhook (public)
func (h *BillingHandlers) Webhook(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	initStripeKey()
	const maxBody = int64(65536)
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "read body"})
		return
	}
	event, err := billing.ParseWebhookEvent(payload, r.Header.Get("Stripe-Signature"))
	if errors.Is(err, billing.ErrWebhookNotConfigured) {
		code, msg := billing.WebhookMissingSecretResponse()
		writeJSON(w, code, map[string]string{"message": msg})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid signature"})
		return
	}
	proc := &billing.Processor{DB: h.svc.DB, Quota: h.svc.Quota}
	if err := proc.ProcessEvent(r.Context(), event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "processing failed"})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func priceForPlan(def domain.QuotaDefaults, plan string) (priceID, normalized string) {
	switch strings.ToLower(plan) {
	case domain.QuotaPlanStarter:
		return def.StripePriceStarter, domain.QuotaPlanStarter
	case domain.QuotaPlanPro:
		return def.StripePricePro, domain.QuotaPlanPro
	default:
		return "", ""
	}
}

