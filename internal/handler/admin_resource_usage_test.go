package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func adminTokenAndRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, fmt.Sprintf("admin-res-%s@example.com", uuid.NewString()[:8]))
	return router, tok
}

// ─── ResourceUsageSummary tests ───────────────────────────────────────────────

// TestResourceUsageSummary_Empty verifies that empty resource_usage_events
// returns a zero-value summary without error.
func TestResourceUsageSummary_Empty(t *testing.T) {
	router, tok := adminTokenAndRouter(t)

	w := authGet(t, router, "/api/admin/resource-usage/summary?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 0, resp["total_credits_burned"])
	assert.EqualValues(t, 0, resp["total_raw_cost_usd_micro"])
	assert.EqualValues(t, 0, resp["total_loaded_cost_usd_micro"])
	assert.Nil(t, resp["browser_automation"])
	assert.Nil(t, resp["document_render"])
}

// TestResourceUsageSummary_KnownEvents inserts events and verifies the aggregate
// response includes correct per-type and total values.
func TestResourceUsageSummary_KnownEvents(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-known@example.com")
	userID := createTestUser(t, db)

	now := time.Now().UTC()
	// Browser: 2 events, 1 credit each, 30000ms each.
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-1*time.Hour)), 1, 30000, true)
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-2*time.Hour)), 1, 30000, true)
	// Render: 1 event, 2 credits.
	seedResourceEvent(t, db, userID, "document_render", isoFmt(now.Add(-3*time.Hour)), 2, 0, true)

	w := authGet(t, router, "/api/admin/resource-usage/summary?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 4, resp["total_credits_burned"])

	ba := resp["browser_automation"].(map[string]any)
	assert.EqualValues(t, 2, ba["events"])
	assert.EqualValues(t, 2, ba["credits_burned"])
	assert.EqualValues(t, 60000, ba["duration_ms"])
	assert.InDelta(t, 1.0, ba["duration_minutes"].(float64), 0.01)

	dr := resp["document_render"].(map[string]any)
	assert.EqualValues(t, 1, dr["events"])
	assert.EqualValues(t, 2, dr["credits_burned"])
}

// TestResourceUsageSummary_PeriodFilter verifies that events outside the
// requested period are excluded.
func TestResourceUsageSummary_PeriodFilter(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-period@example.com")
	userID := createTestUser(t, db)

	// Event inside 7d window.
	seedResourceEvent(t, db, userID, "browser_automation",
		isoFmt(time.Now().UTC().Add(-3*24*time.Hour)), 5, 30000, true)
	// Event 40 days ago — outside 30d, outside 7d.
	seedResourceEvent(t, db, userID, "browser_automation",
		isoFmt(time.Now().UTC().Add(-40*24*time.Hour)), 999, 30000, true)

	w := authGet(t, router, "/api/admin/resource-usage/summary?period=7d", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 5, resp["total_credits_burned"],
		"only in-period events must contribute to the summary")
}

// ─── ResourceUsageEvents tests ────────────────────────────────────────────────

// TestResourceUsageEvents_Empty verifies empty DB returns empty events list.
func TestResourceUsageEvents_Empty(t *testing.T) {
	router, tok := adminTokenAndRouter(t)

	w := authGet(t, router, "/api/admin/resource-usage/events?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 0, resp["total"])
	events := resp["events"].([]any)
	assert.Empty(t, events)
}

// TestResourceUsageEvents_DiagnosticFields verifies that success, error_code,
// application_id, automation_run_id, and job_id are returned in each event row.
func TestResourceUsageEvents_DiagnosticFields(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-diag@example.com")
	userID := createTestUser(t, db)

	evID := uuid.NewString()
	_, err := db.Exec(`
		INSERT INTO resource_usage_events
			(id, user_id, resource_type, quantity, unit, duration_ms,
			 raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned,
			 success, error_code, automation_run_id, application_id, job_id, created_at)
		VALUES (?, ?, 'browser_automation', 1.0, 'increment', 30000,
			100, 150, 1, 0, 'quota_exceeded', 'run-abc', 'app-xyz', 'job-123', datetime('now'))`,
		evID, userID,
	)
	require.NoError(t, err)

	w := authGet(t, router, "/api/admin/resource-usage/events?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	events := resp["events"].([]any)
	require.Len(t, events, 1)

	ev := events[0].(map[string]any)
	assert.Equal(t, evID, ev["id"])
	assert.Equal(t, false, ev["success"])
	assert.Equal(t, "quota_exceeded", ev["error_code"])
	assert.Equal(t, "run-abc", ev["automation_run_id"])
	assert.Equal(t, "app-xyz", ev["application_id"])
	assert.Equal(t, "job-123", ev["job_id"])
}

// TestResourceUsageEvents_NoPrivateFields verifies that metadata_json,
// idempotency_key, and operation_id are NOT present in the event row.
func TestResourceUsageEvents_NoPrivateFields(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-noprivate@example.com")
	userID := createTestUser(t, db)

	seedResourceEvent(t, db, userID, "document_render", isoFmt(time.Now().UTC()), 1, 0, true)

	w := authGet(t, router, "/api/admin/resource-usage/events?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	events := resp["events"].([]any)
	require.Len(t, events, 1)
	ev := events[0].(map[string]any)

	_, hasMetadata := ev["metadata_json"]
	_, hasIdempotency := ev["idempotency_key"]
	_, hasOperation := ev["operation_id"]
	assert.False(t, hasMetadata, "metadata_json must not be exposed")
	assert.False(t, hasIdempotency, "idempotency_key must not be exposed")
	assert.False(t, hasOperation, "operation_id must not be exposed")
}

// TestResourceUsageEvents_Pagination verifies total, limit, offset fields and
// that page contents match the expected subset.
func TestResourceUsageEvents_Pagination(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-page@example.com")
	userID := createTestUser(t, db)

	// Insert 5 events at different times so ORDER BY created_at DESC is deterministic.
	for i := 0; i < 5; i++ {
		seedResourceEvent(t, db, userID, "browser_automation",
			isoFmt(time.Now().UTC().Add(-time.Duration(i+1)*time.Hour)), 1, 30000, true)
	}

	// Page 1: limit=2, offset=0 → 2 events.
	w := authGet(t, router, "/api/admin/resource-usage/events?period=30d&limit=2&offset=0", tok)
	require.Equal(t, http.StatusOK, w.Code)
	var page1 map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&page1))
	assert.EqualValues(t, 5, page1["total"])
	assert.EqualValues(t, 2, page1["limit"])
	assert.EqualValues(t, 0, page1["offset"])
	assert.Len(t, page1["events"].([]any), 2)

	// Page 3: limit=2, offset=4 → 1 remaining event.
	w2 := authGet(t, router, "/api/admin/resource-usage/events?period=30d&limit=2&offset=4", tok)
	require.Equal(t, http.StatusOK, w2.Code)
	var page3 map[string]any
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&page3))
	assert.EqualValues(t, 5, page3["total"])
	assert.Len(t, page3["events"].([]any), 1)
}

// TestResourceUsageEvents_UserFilter verifies that the user_id query param
// restricts results to that user's events.
func TestResourceUsageEvents_UserFilter(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-res-ufilt@example.com")
	userA := createTestUser(t, db)
	userB := createTestUser(t, db)

	now := time.Now().UTC()
	seedResourceEvent(t, db, userA, "browser_automation", isoFmt(now.Add(-1*time.Hour)), 5, 30000, true)
	seedResourceEvent(t, db, userB, "document_render", isoFmt(now.Add(-2*time.Hour)), 3, 0, true)

	w := authGet(t, router, "/api/admin/resource-usage/events?period=30d&user_id="+userA, tok)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.EqualValues(t, 1, resp["total"], "filter must return only user A's events")
	events := resp["events"].([]any)
	require.Len(t, events, 1)
	assert.Equal(t, userA, events[0].(map[string]any)["user_id"])
}

// ─── EconomicsOverview unified totals test ────────────────────────────────────

// TestEconomicsOverview_UnifiedTotals verifies that the extended economics
// overview includes ai, resource sub-objects and total_* unified fields.
func TestEconomicsOverview_UnifiedTotals(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	tok := adminTokenFor(t, router, db, "admin-eco-totals@example.com")
	userID := createTestUser(t, db)

	now := time.Now().UTC()
	// AI event: 10 credits.
	seedLLMEvent(t, db, userID, isoFmt(now.Add(-1*time.Hour)), 10, false)
	// Browser event: 3 credits.
	seedResourceEvent(t, db, userID, "browser_automation", isoFmt(now.Add(-2*time.Hour)), 3, 30000, true)
	// Render event: 2 credits.
	seedResourceEvent(t, db, userID, "document_render", isoFmt(now.Add(-3*time.Hour)), 2, 0, true)

	w := authGet(t, router, "/api/admin/economics/overview?period=30d", tok)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Legacy fields still present.
	assert.NotNil(t, resp["credits_burned"], "legacy credits_burned must remain")
	assert.NotNil(t, resp["calls"], "legacy calls must remain")

	// ai sub-object.
	ai := resp["ai"].(map[string]any)
	assert.EqualValues(t, 10, ai["credits_burned"])

	// resource sub-object.
	res := resp["resource"].(map[string]any)
	assert.EqualValues(t, 5, res["credits_burned"])  // 3 + 2
	assert.EqualValues(t, 1, res["browser_events"])
	assert.EqualValues(t, 1, res["render_events"])
	assert.EqualValues(t, 30000, res["browser_duration_ms"])

	// Unified totals: ai (10) + resource (5) = 15.
	assert.EqualValues(t, 15, resp["total_credits_burned"])
}
