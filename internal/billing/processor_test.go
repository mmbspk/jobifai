package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v82"
	"github.com/user/jobifai/internal/billing"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/quota"
)

func testQuota(t *testing.T) (*sql.DB, *quota.Service, string) {
	t.Helper()
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.NewStore(db)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, display_name, is_admin, created_at) VALUES ('u1','a@t.com','x','A',0,CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	svc := quota.NewService(db, cfg, nil)
	require.NoError(t, svc.InitTrial(context.Background(), "u1"))
	def := domain.QuotaDefaults{
		StarterCreditsMonthly: 3000, ProCreditsMonthly: 8000,
		StripePriceStarter: "price_starter", StripePricePro: "price_pro",
	}
	require.NoError(t, svc.SaveDefaults(def))
	return db, svc, "u1"
}

func TestProcessor_TopUpIdempotent(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	raw := `{"id":"cs_1","object":"checkout.session","mode":"payment","payment_status":"paid","metadata":{"jobifai_user_id":"u1","jobifai_topup":"1","jobifai_credits":"500"}}`
	ev := stripe.Event{ID: "evt_top_1", Created: 100, Type: stripe.EventTypeCheckoutSessionCompleted, Data: &stripe.EventData{Raw: json.RawMessage(raw)}}
	require.NoError(t, proc.ProcessEvent(context.Background(), ev))
	row, err := svc.RowForUser(uid)
	require.NoError(t, err)
	assert.Equal(t, int64(500), row.TopUpCreditsRemaining)
	ev2 := ev
	ev2.ID = "evt_top_1"
	require.NoError(t, proc.ProcessEvent(context.Background(), ev2))
	row2, _ := svc.RowForUser(uid)
	assert.Equal(t, int64(500), row2.TopUpCreditsRemaining)
}

func TestProcessor_SubscriptionSamePeriodNoReset(t *testing.T) {
	db, svc, uid := testQuota(t)
	start := time.Now().Unix()
	end := start + 86400*30
	subRaw, _ := json.Marshal(map[string]any{
		"id": "sub_1", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_starter"},
			"current_period_start": start, "current_period_end": end,
		}}},
	})
	proc := &billing.Processor{DB: db, Quota: svc}
	ev := stripe.Event{ID: "evt_sub_1", Created: 100, Type: stripe.EventTypeCustomerSubscriptionUpdated, Data: &stripe.EventData{Raw: subRaw}}
	require.NoError(t, proc.ProcessEvent(context.Background(), ev))
	_, err := db.Exec(`UPDATE user_quota SET period_used_micro = 900 WHERE user_id = ?`, uid)
	require.NoError(t, err)
	ev2 := stripe.Event{ID: "evt_sub_2", Created: 101, Type: stripe.EventTypeCustomerSubscriptionUpdated, Data: &stripe.EventData{Raw: subRaw}}
	require.NoError(t, proc.ProcessEvent(context.Background(), ev2))
	row2, _ := svc.RowForUser(uid)
	assert.Equal(t, int64(900), row2.PeriodUsedCredits)
	assert.Equal(t, domain.QuotaPlanStarter, row2.Plan)
}

func TestProcessor_UnknownPriceFailsClosed(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	start := time.Now().Unix()
	subRaw, _ := json.Marshal(map[string]any{
		"id": "sub_bad", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_unknown"},
			"current_period_start": start, "current_period_end": start + 86400 * 30,
		}}},
	})
	ev := stripe.Event{ID: "evt_bad_price", Created: 100, Type: stripe.EventTypeCustomerSubscriptionUpdated, Data: &stripe.EventData{Raw: subRaw}}
	require.Error(t, proc.ProcessEvent(context.Background(), ev))
	row, _ := svc.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanTrial, row.Plan)
}

func TestProcessor_DuplicateEventIdempotent(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	start := time.Now().Unix()
	subRaw, _ := json.Marshal(map[string]any{
		"id": "sub_dup", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_starter"},
			"current_period_start": start, "current_period_end": start + 86400 * 30,
		}}},
	})
	ev := stripe.Event{ID: "evt_dup_sub", Created: 100, Type: stripe.EventTypeCustomerSubscriptionCreated, Data: &stripe.EventData{Raw: subRaw}}
	require.NoError(t, proc.ProcessEvent(context.Background(), ev))
	row, _ := svc.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanStarter, row.Plan)
	require.NoError(t, proc.ProcessEvent(context.Background(), ev))
	row2, _ := svc.RowForUser(uid)
	assert.Equal(t, row.PeriodUsedCredits, row2.PeriodUsedCredits)
}

func TestProcessor_StaleSubscriptionUpdateAfterDelete(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	start := time.Now().Unix()
	subActive, _ := json.Marshal(map[string]any{
		"id": "sub_stale", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_starter"},
			"current_period_start": start, "current_period_end": start + 86400 * 30,
		}}},
	})
	subDel, _ := json.Marshal(map[string]any{
		"id": "sub_stale", "object": "subscription", "status": "canceled",
		"metadata": map[string]string{"jobifai_user_id": uid},
	})
	require.NoError(t, proc.ProcessEvent(context.Background(), stripe.Event{
		ID: "evt_del", Created: 2000, Type: stripe.EventTypeCustomerSubscriptionDeleted,
		Data: &stripe.EventData{Raw: subDel},
	}))
	require.NoError(t, proc.ProcessEvent(context.Background(), stripe.Event{
		ID: "evt_old", Created: 1000, Type: stripe.EventTypeCustomerSubscriptionUpdated,
		Data: &stripe.EventData{Raw: subActive},
	}))
	row, _ := svc.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanExpired, row.Plan)
}

func TestProcessor_InvoicePaidReconcilesPastDue(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	start := time.Now().Unix()
	billing.SetFetchSubscriptionForTest(func(subID string) (stripe.Subscription, error) {
		raw, _ := json.Marshal(map[string]any{
			"id": subID, "object": "subscription", "status": "active",
			"metadata": map[string]string{"jobifai_user_id": uid},
			"items": map[string]any{"data": []map[string]any{{
				"price": map[string]string{"id": "price_starter"},
				"current_period_start": start, "current_period_end": start + 86400 * 30,
			}}},
		})
		var sub stripe.Subscription
		_ = json.Unmarshal(raw, &sub)
		return sub, nil
	})
	t.Cleanup(func() { billing.SetFetchSubscriptionForTest(nil) })

	invRaw, _ := json.Marshal(map[string]any{
		"object": "invoice",
		"parent": map[string]any{
			"subscription_details": map[string]any{
				"subscription": map[string]string{"id": "sub_inv"},
			},
		},
	})
	require.NoError(t, proc.ProcessEvent(context.Background(), stripe.Event{
		ID: "evt_inv_paid", Created: 500, Type: stripe.EventTypeInvoicePaid,
		Data: &stripe.EventData{Raw: invRaw},
	}))
	row, _ := svc.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanStarter, row.Plan)
	assert.Equal(t, "active", row.StripeSubscriptionStatus)
}

func TestProcessor_FailedWebhookCanRetry(t *testing.T) {
	db, svc, uid := testQuota(t)
	proc := &billing.Processor{DB: db, Quota: svc}
	start := time.Now().Unix()
	badSub, _ := json.Marshal(map[string]any{
		"id": "sub_retry", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_unknown"},
			"current_period_start": start, "current_period_end": start + 86400 * 30,
		}}},
	})
	ev := stripe.Event{ID: "evt_retry", Created: 100, Type: stripe.EventTypeCustomerSubscriptionUpdated, Data: &stripe.EventData{Raw: badSub}}
	require.Error(t, proc.ProcessEvent(context.Background(), ev))
	goodSub, _ := json.Marshal(map[string]any{
		"id": "sub_retry", "object": "subscription", "status": "active",
		"metadata": map[string]string{"jobifai_user_id": uid},
		"items": map[string]any{"data": []map[string]any{{
			"price": map[string]string{"id": "price_starter"},
			"current_period_start": start, "current_period_end": start + 86400 * 30,
		}}},
	})
	ev2 := stripe.Event{ID: "evt_retry", Created: 100, Type: stripe.EventTypeCustomerSubscriptionUpdated, Data: &stripe.EventData{Raw: goodSub}}
	require.NoError(t, proc.ProcessEvent(context.Background(), ev2))
	row, _ := svc.RowForUser(uid)
	assert.Equal(t, domain.QuotaPlanStarter, row.Plan)
}

func TestWebhook_RequiresSecretUnlessInsecure(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	t.Setenv("JOBIFAI_STRIPE_WEBHOOK_INSECURE", "")
	_, err := billing.ParseWebhookEvent([]byte(`{"id":"evt_x","type":"ping"}`), "")
	require.ErrorIs(t, err, billing.ErrWebhookNotConfigured)
}
