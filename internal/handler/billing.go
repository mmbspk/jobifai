package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stripe/stripe-go/v82"
	billingportalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// BillingHandlers Stripe checkout + webhooks.
type BillingHandlers struct{ svc *Services }

func NewBillingHandlers(svc *Services) *BillingHandlers { return &BillingHandlers{svc: svc} }

func stripeEnabled() bool {
	return os.Getenv("STRIPE_SECRET_KEY") != ""
}

func initStripeKey() {
	if k := os.Getenv("STRIPE_SECRET_KEY"); k != "" {
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
	baseURL := appBaseURL()
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
	baseURL := appBaseURL()
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

func appBaseURL() string {
	baseURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8081"
	}
	return baseURL
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
		ReturnURL: stripe.String(appBaseURL() + "/settings/plan"),
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
	secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	var event stripe.Event
	if secret != "" {
		event, err = webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), secret)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid signature"})
			return
		}
	} else if err := json.Unmarshal(payload, &event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid event"})
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutCompleted(event)
	case "customer.subscription.updated", "customer.subscription.created":
		h.handleSubscription(event)
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

func (h *BillingHandlers) handleCheckoutCompleted(event stripe.Event) {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return
	}
	userID := sess.Metadata["jobifai_user_id"]
	if userID == "" && sess.Customer != nil {
		if row, err := h.svc.Quota.RowByStripeCustomer(sess.Customer.ID); err == nil {
			userID = row.UserID
		}
	}
	if userID == "" {
		return
	}
	if sess.Customer != nil && sess.Customer.ID != "" {
		_ = h.svc.Quota.SetStripeCustomer(userID, sess.Customer.ID)
	}
	if sess.Metadata["jobifai_topup"] == "1" {
		credits, _ := strconv.ParseInt(sess.Metadata["jobifai_credits"], 10, 64)
		if credits > 0 {
			_ = h.svc.Quota.AddTopUpCredits(userID, credits)
		}
	}
}

func (h *BillingHandlers) handleSubscription(event stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return
	}
	h.applySubscription(sub)
}

func (h *BillingHandlers) applySubscription(sub stripe.Subscription) {
	userID := ""
	if sub.Metadata != nil {
		userID = sub.Metadata["jobifai_user_id"]
	}
	custID := ""
	if sub.Customer != nil {
		custID = sub.Customer.ID
	}
	if userID == "" && custID != "" {
		if row, err := h.svc.Quota.RowByStripeCustomer(custID); err == nil {
			userID = row.UserID
		}
	}
	if userID == "" {
		return
	}
	plan := domain.QuotaPlanStarter
	priceID := ""
	if len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		priceID = sub.Items.Data[0].Price.ID
	}
	def := h.svc.Quota.LoadDefaults()
	switch priceID {
	case def.StripePricePro:
		plan = domain.QuotaPlanPro
	case def.StripePriceStarter:
		plan = domain.QuotaPlanStarter
	}
	allowance := quota.AllowanceCreditsForPlan(def, plan)
	var start, end int64
	if len(sub.Items.Data) > 0 {
		start = sub.Items.Data[0].CurrentPeriodStart
		end = sub.Items.Data[0].CurrentPeriodEnd
	}
	_ = h.svc.Quota.ApplySubscriptionPeriod(userID, plan, custID, sub.ID, start, end, allowance)
}
