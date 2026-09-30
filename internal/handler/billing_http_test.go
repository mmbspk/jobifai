package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/quota"
)

func billingTestRouter(t *testing.T) (http.Handler, *handler.Services, string) {
	t.Helper()
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_fake")
	svc, db := newTestServices(t)
	svc.Quota = quota.NewService(db, svc.Config, svc.Users)
	require.NoError(t, svc.Quota.SaveDefaults(domain.QuotaDefaults{
		EnforcementDefault: true,
		TrialCredits:       500,
		TrialDays:          7,
		StripePriceStarter: "price_starter",
		StripePricePro:     "price_pro",
		TopUpPacks:         []domain.TopUpPack{{Credits: 1000, PriceID: "price_top_1000"}},
	}))
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "bill@example.com", "password123")
	users, _ := svc.Users.ByEmail("bill@example.com")
	require.NoError(t, svc.Quota.InitTrial(context.Background(), users.ID))
	return router, svc, token
}

func TestBilling_TopUpRejectedForTrial(t *testing.T) {
	router, _, token := billingTestRouter(t)
	w := authPost(t, router, "/api/billing/topup", token, map[string]any{"credits": 1000})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "top-ups require an active paid subscription")
}

func TestBilling_TopUpRejectedForExpired(t *testing.T) {
	router, svc, token := billingTestRouter(t)
	u, _ := svc.Users.ByEmail("bill@example.com")
	require.NoError(t, svc.Quota.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: u.ID, EventID: "e1", EventCreatedUnix: 100, Terminate: true, Status: "canceled",
	}))
	w := authPost(t, router, "/api/billing/topup", token, map[string]any{"credits": 1000})
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestBilling_CheckoutRejectedWhenSubscriptionActive(t *testing.T) {
	router, svc, token := billingTestRouter(t)
	u, _ := svc.Users.ByEmail("bill@example.com")
	def := svc.Quota.LoadDefaults()
	def.StripePriceStarter = "price_starter"
	def.StripePricePro = "price_pro"
	require.NoError(t, svc.Quota.SaveDefaults(def))
	start := int64(1_700_000_000)
	require.NoError(t, svc.Quota.ApplyStripeSubscription(quota.StripeSubscriptionUpdate{
		UserID: u.ID, EventID: "e1", EventCreatedUnix: 100,
		Plan: domain.QuotaPlanStarter, PriceID: "price_starter", Status: "active",
		SubscriptionID: "sub_existing", PeriodStartUnix: start, PeriodEndUnix: start + 86400*30,
		AllowanceCredits: 3000,
	}))
	w := authPost(t, router, "/api/billing/checkout", token, map[string]any{"plan": "pro"})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already have a subscription")
}
