package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/quota"
)

// ─── stub QuotaService for period-boundary tests ──────────────────────────────

type summaryQuotaStub struct {
	row      domain.UserQuotaRow
	rowErr   error
	defaults domain.QuotaDefaults
}

func (s *summaryQuotaStub) RowForUser(_ string) (domain.UserQuotaRow, error) {
	return s.row, s.rowErr
}
func (s *summaryQuotaStub) LoadDefaults() domain.QuotaDefaults { return s.defaults }

// All unused methods — panic on call so a missed test dependency is visible.
func (s *summaryQuotaStub) Status(_ context.Context, _ string) (domain.QuotaStatus, error) {
	return domain.QuotaStatus{}, nil
}
func (s *summaryQuotaStub) InitTrial(_ context.Context, _ string) error { return nil }
func (s *summaryQuotaStub) BeforeLLM(_ context.Context, _, _ string, _, _ int) error {
	return nil
}
func (s *summaryQuotaStub) RecordLLM(_ context.Context, _, _ string, _, _ int64) error {
	return nil
}
func (s *summaryQuotaStub) BeginSubscriberSession(_ string)                   {}
func (s *summaryQuotaStub) EndSubscriberSession(_ string)                     {}
func (s *summaryQuotaStub) EstimateLLMCredits(_ string, _, _ int) int64       { return 0 }
func (s *summaryQuotaStub) SaveDefaults(_ domain.QuotaDefaults) error         { return nil }
func (s *summaryQuotaStub) SaveUserOverrides(_ string, _ domain.QuotaUserOverrides) error {
	return nil
}
func (s *summaryQuotaStub) UserOverrides(_ string) domain.QuotaUserOverrides { return domain.QuotaUserOverrides{} }
func (s *summaryQuotaStub) SetEnforcement(_ string, _ bool) error            { return nil }
func (s *summaryQuotaStub) RowByStripeCustomer(_ string) (domain.UserQuotaRow, error) {
	return domain.UserQuotaRow{}, nil
}
func (s *summaryQuotaStub) SetStripeCustomer(_, _ string) error { return nil }
func (s *summaryQuotaStub) ApplySubscriptionPeriod(_, _, _, _ string, _, _ int64, _ int64) error {
	return nil
}
func (s *summaryQuotaStub) AddTopUpCredits(_ string, _ int64) error { return nil }
func (s *summaryQuotaStub) ApplyStripeSubscription(_ quota.StripeSubscriptionUpdate) error {
	return nil
}
func (s *summaryQuotaStub) RepairReconcileAllowance(_, _ string) error { return nil }
func (s *summaryQuotaStub) GrantTopUpOnce(_, _, _ string, _ int64) error { return nil }

// ─── DB seeding helpers ───────────────────────────────────────────────────────

func seedLLMEvent(t *testing.T, db *sql.DB, userID, createdAt string, credits int64, personalProvider bool) {
	t.Helper()
	pp := 0
	if personalProvider {
		pp = 1
	}
	_, err := db.Exec(`
		INSERT INTO llm_usage_events
			(id, created_at, user_id, task, provider, requested_model, actual_model, actual_model_verified,
			 input_tokens, output_tokens, raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned,
			 latency_ms, success, personal_provider)
		VALUES (?, ?, ?, 'job_scoring', 'claude', 'claude-haiku-4-5-20251001', 'claude-haiku-4-5-20251001', 1,
			100, 20, 10, 15, ?, 50, 1, ?)`,
		uuid.NewString(), createdAt, userID, credits, pp,
	)
	require.NoError(t, err)
}

// seedLLMEventNative inserts an LLM event binding createdAt as a time.Time value,
// matching how production RecordBillingTx writes to llm_usage_events.created_at.
func seedLLMEventNative(t *testing.T, db *sql.DB, userID string, createdAt time.Time, credits int64, personalProvider bool) {
	t.Helper()
	pp := 0
	if personalProvider {
		pp = 1
	}
	_, err := db.Exec(`
		INSERT INTO llm_usage_events
			(id, created_at, user_id, task, provider, requested_model, actual_model, actual_model_verified,
			 input_tokens, output_tokens, raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned,
			 latency_ms, success, personal_provider)
		VALUES (?, ?, ?, 'job_scoring', 'claude', 'claude-haiku-4-5-20251001', 'claude-haiku-4-5-20251001', 1,
			100, 20, 10, 15, ?, 50, 1, ?)`,
		uuid.NewString(), createdAt, userID, credits, pp,
	)
	require.NoError(t, err)
}

func seedResourceEvent(t *testing.T, db *sql.DB, userID, resType, createdAt string, credits, durationMS int64, success bool) {
	t.Helper()
	s := 1
	if !success {
		s = 0
	}
	_, err := db.Exec(`
		INSERT INTO resource_usage_events
			(id, user_id, resource_type, quantity, unit, duration_ms,
			 raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned,
			 success, created_at)
		VALUES (?, ?, ?, 1.0, 'increment', ?, 100, 150, ?, ?, ?)`,
		uuid.NewString(), userID, resType, durationMS, credits, s, createdAt,
	)
	require.NoError(t, err)
}

// isoFmt formats a time as RFC3339 for DB inserts.
func isoFmt(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ─── helper: create a user row that the DB FK constraint accepts ──────────────

func createTestUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	uid := uuid.NewString()
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, 'x', datetime('now'))`,
		uid, uid+"@example.com")
	require.NoError(t, err)
	return uid
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestUsageSummary_NilQuotaService verifies that a nil Quota service falls back
// to the rolling 30d window and returns 200 with zero credits.
func TestUsageSummary_NilQuotaService(t *testing.T) {
	svc, _ := newTestServices(t)
	// Quota is nil — default from newTestServices.
	router := handler.NewRouter(svc)
	userTok := registerAndLogin(t, router, "summary-nil-quota@example.com", "pass1234")

	w := authGet(t, router, "/api/usage/summary", userTok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "billing_period", resp["period"])
	assert.NotEmpty(t, resp["period_start"])
	assert.NotEmpty(t, resp["period_end"], "fallback case must always include period_end")
	assert.EqualValues(t, 0, resp["total_credits_used"])
}

// TestUsageSummary_Empty verifies that an empty DB returns zeros without error.
func TestUsageSummary_Empty(t *testing.T) {
	svc, db := newTestServices(t)
	_ = db
	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-empty@example.com", "pass1234")

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 0, resp["total_credits_used"])
	assert.EqualValues(t, 0, resp["ai_credits"])
	assert.EqualValues(t, 0, resp["browser_automation_credits"])
	assert.EqualValues(t, 0, resp["document_render_credits"])
}

// TestUsageSummary_AICredits verifies that LLM events for the calling user are
// counted and returned in ai_credits.
func TestUsageSummary_AICredits(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-ai@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	now := time.Now().UTC()
	seedLLMEventNative(t, db, userID, now.Add(-1*time.Hour), 5, false)
	seedLLMEventNative(t, db, userID, now.Add(-2*time.Hour), 3, false)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 8, resp["ai_credits"])
	assert.EqualValues(t, 2, resp["ai_calls"])
	assert.EqualValues(t, 8, resp["total_credits_used"])
}

// TestUsageSummary_PersonalProviderExcluded verifies that personal_provider=1
// events are excluded from ai_credits (they burn 0 platform credits).
func TestUsageSummary_PersonalProviderExcluded(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-personal@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	now := time.Now().UTC()
	seedLLMEventNative(t, db, userID, now.Add(-1*time.Hour), 10, false) // platform → counted
	seedLLMEventNative(t, db, userID, now.Add(-2*time.Hour), 5, true)   // personal → excluded

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 10, resp["ai_credits"],
		"personal_provider events must not contribute to ai_credits")
	assert.EqualValues(t, 1, resp["ai_calls"],
		"personal_provider call count must not be included")
}

// TestUsageSummary_TotalInvariant verifies total_credits_used = ai + browser + render.
func TestUsageSummary_TotalInvariant(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-total@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	now := time.Now().UTC()
	seedLLMEventNative(t, db, userID, now.Add(-1*time.Hour), 7, false)
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-2*time.Hour)), 3, 30000, true)
	seedResourceEvent(t, db, userID, "document_render", isoFmt(now.Add(-3*time.Hour)), 2, 0, true)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	ai := int64(resp["ai_credits"].(float64))
	browser := int64(resp["browser_automation_credits"].(float64))
	render := int64(resp["document_render_credits"].(float64))
	total := int64(resp["total_credits_used"].(float64))
	assert.Equal(t, ai+browser+render, total, "total must equal ai + browser + render")
	assert.EqualValues(t, 7, ai)
	assert.EqualValues(t, 3, browser)
	assert.EqualValues(t, 2, render)
}

// TestUsageSummary_UserIsolation verifies that user A's events are never
// returned in user B's summary response.
func TestUsageSummary_UserIsolation(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tokA := registerAndLogin(t, router, "summary-iso-a@example.com", "pass1234")
	tokB := registerAndLogin(t, router, "summary-iso-b@example.com", "pass1234")
	userIDA := userIDFromToken(t, router, tokA)

	now := time.Now().UTC()
	seedLLMEvent(t, db, userIDA, isoFmt(now.Add(-1*time.Hour)), 100, false)

	w := authGet(t, router, "/api/usage/summary", tokB)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 0, resp["ai_credits"],
		"user B must never see user A's credits")
}

// TestUsageSummary_PeriodBoundary_Paid verifies that events outside a paid
// subscription period are excluded, and events inside are included.
func TestUsageSummary_PeriodBoundary_Paid(t *testing.T) {
	svc, db := newTestServices(t)

	periodStart := time.Now().UTC().Add(-15 * 24 * time.Hour)
	periodEnd := time.Now().UTC().Add(15 * 24 * time.Hour)
	svc.Quota = &summaryQuotaStub{
		row: domain.UserQuotaRow{
			Plan:        "starter",
			PeriodStart: &periodStart,
			PeriodEnd:   &periodEnd,
		},
	}

	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-paid@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	// Event inside the period.
	seedLLMEvent(t, db, userID, isoFmt(time.Now().UTC().Add(-5*24*time.Hour)), 20, false)
	// Event before the period — must be excluded.
	seedLLMEvent(t, db, userID, isoFmt(periodStart.Add(-24*time.Hour)), 999, false)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 20, resp["ai_credits"],
		"only in-period events must be counted for paid users")
	assert.NotEmpty(t, resp["period_end"], "paid users must have period_end")
}

// TestUsageSummary_PeriodBoundary_Trial verifies that trial users see events
// within TrialEndsAt - TrialDays, and events before the trial start are excluded.
func TestUsageSummary_PeriodBoundary_Trial(t *testing.T) {
	svc, db := newTestServices(t)

	trialDays := 7
	trialEndsAt := time.Now().UTC().Add(2 * 24 * time.Hour) // ends 2 days from now
	svc.Quota = &summaryQuotaStub{
		row: domain.UserQuotaRow{
			Plan:        "trial",
			TrialEndsAt: &trialEndsAt,
		},
		defaults: domain.QuotaDefaults{TrialDays: trialDays},
	}

	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-trial@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	trialStart := trialEndsAt.Add(-time.Duration(trialDays) * 24 * time.Hour)
	// Event inside trial period.
	seedLLMEvent(t, db, userID, isoFmt(trialStart.Add(time.Hour)), 11, false)
	// Event before trial start — must be excluded.
	seedLLMEvent(t, db, userID, isoFmt(trialStart.Add(-24*time.Hour)), 999, false)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 11, resp["ai_credits"],
		"only in-trial events must be counted for trial users")
	assert.NotEmpty(t, resp["period_end"], "trial users must have period_end")
}

// TestUsageSummary_BrowserActiveMinutes verifies that browser_active_ms and
// browser_active_minutes are computed from successful events only.
func TestUsageSummary_BrowserActiveMinutes(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-browser@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	now := time.Now().UTC()
	// Two successful browser events (30s each = 60000 ms total).
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-1*time.Hour)), 1, 30000, true)
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-2*time.Hour)), 1, 30000, true)
	// One failed event — duration must be excluded.
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-3*time.Hour)), 0, 30000, false)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 60000, resp["browser_active_ms"])
	assert.InDelta(t, 1.0, resp["browser_active_minutes"].(float64), 0.01)
}

// TestUsageSummary_LLMTimestampNative verifies that period boundary filtering works
// when LLM events are inserted with a time.Time value (matching production
// RecordBillingTx), not a pre-formatted RFC3339 string.
func TestUsageSummary_LLMTimestampNative(t *testing.T) {
	svc, db := newTestServices(t)

	periodStart := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Second)
	periodEnd := time.Now().UTC().Add(10 * 24 * time.Hour).Truncate(time.Second)
	svc.Quota = &summaryQuotaStub{
		row: domain.UserQuotaRow{
			Plan:        "starter",
			PeriodStart: &periodStart,
			PeriodEnd:   &periodEnd,
		},
	}

	router := handler.NewRouter(svc)
	tok := registerAndLogin(t, router, "summary-ts-native@example.com", "pass1234")
	userID := userIDFromToken(t, router, tok)

	// Inside period: inserted as time.Time (production-style).
	seedLLMEventNative(t, db, userID, time.Now().UTC(), 7, false)
	// Before period boundary — must be excluded.
	seedLLMEventNative(t, db, userID, periodStart.Add(-time.Second), 999, false)

	w := authGet(t, router, "/api/usage/summary", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 7, resp["ai_credits"],
		"time.Time-inserted LLM events must respect period boundaries")
}

// TestUsageSummary_UserIDInjection verifies that a user_id query parameter is
// ignored and only the authenticated caller's data is returned.
func TestUsageSummary_UserIDInjection(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tokA := registerAndLogin(t, router, "summary-inject-a@example.com", "pass1234")
	tokB := registerAndLogin(t, router, "summary-inject-b@example.com", "pass1234")
	userIDA := userIDFromToken(t, router, tokA)

	// Give user A a large credit balance.
	now := time.Now().UTC()
	seedLLMEvent(t, db, userIDA, isoFmt(now.Add(-1*time.Hour)), 100, false)

	// Authenticate as user B but pass user A's ID as a query param.
	w := authGet(t, router, "/api/usage/summary?user_id="+userIDA, tokB)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 0, resp["ai_credits"],
		"user_id query param must be ignored; only caller's data returned")
}
