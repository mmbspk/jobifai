package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

type stubUsageStore struct{ snap domain.SessionUsage }

func (s *stubUsageStore) Session(_ string) domain.SessionUsage { return s.snap }

func TestUsage_Session_ReturnsSnapshot(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.UsageStore = &stubUsageStore{snap: domain.SessionUsage{InputTokens: 100, OutputTokens: 50, Calls: 2}}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "usage1@example.com", "password123")

	w := authGet(t, router, "/api/usage/session", token)
	assert.Equal(t, 200, w.Code)

	var resp domain.SessionUsage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, int64(100), resp.InputTokens)
	assert.Equal(t, int64(50), resp.OutputTokens)
	assert.Equal(t, 2, resp.Calls)
}

func TestUsage_Session_NoCostWhenNoConfig(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.UsageStore = &stubUsageStore{snap: domain.SessionUsage{InputTokens: 1000, OutputTokens: 500}}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "usage2@example.com", "password123")

	w := authGet(t, router, "/api/usage/session", token)
	assert.Equal(t, 200, w.Code)

	var resp domain.SessionUsage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Nil(t, resp.EstimatedCostUSD, "no cost when model config is absent")
}

func TestUsage_Session_CostCalculated(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.UsageStore = &stubUsageStore{snap: domain.SessionUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "usage3@example.com", "password123")

	// Set a known model so cost is calculated.
	w := authPost(t, router, "/api/settings/general", token, map[string]any{
		"llm": map[string]any{"provider": "claude", "model": "claude-sonnet-4"},
	})
	require.Equal(t, 200, w.Code)

	w = authGet(t, router, "/api/usage/session", token)
	assert.Equal(t, 200, w.Code)

	var resp domain.SessionUsage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.EstimatedCostUSD, "cost should be set for known model")
	assert.Greater(t, *resp.EstimatedCostUSD, 0.0)
}

func TestUsage_Totals_ZeroReturnsOK(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.UsageStore = &stubUsageStore{}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "usage4@example.com", "password123")

	w := authGet(t, router, "/api/usage/totals", token)
	assert.Equal(t, 200, w.Code)

	var resp domain.SessionUsage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, int64(0), resp.InputTokens)
	assert.Equal(t, int64(0), resp.OutputTokens)
}

func TestUsage_Totals_ReflectsInserted(t *testing.T) {
	svc, sqldb := newTestServices(t)
	svc.UsageStore = &stubUsageStore{}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "usage5@example.com", "password123")

	// Get userID.
	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	require.NoError(t, db.IncrementUsage(sqldb, userID, "claude-sonnet-4-6", 500, 200, 3))

	w := authGet(t, router, "/api/usage/totals", token)
	assert.Equal(t, 200, w.Code)

	var resp domain.SessionUsage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, int64(500), resp.InputTokens)
	assert.Equal(t, int64(200), resp.OutputTokens)
	assert.Equal(t, 3, resp.Calls)
}

func TestUsage_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.UsageStore = &stubUsageStore{}
	router := handler.NewRouter(svc)

	assert.Equal(t, 401, authGet(t, router, "/api/usage/session", "").Code)
	assert.Equal(t, 401, authGet(t, router, "/api/usage/totals", "").Code)
}
