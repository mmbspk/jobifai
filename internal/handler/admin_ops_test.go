package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

func TestAdmin_OverviewAndHealth(t *testing.T) {
	svc, db := newTestServices(t)
	svc.StartedAt = time.Now().Add(-time.Hour)
	router := handler.NewRouter(svc)
	adminToken := registerAndLogin(t, router, "admin-overview@example.com", "password123")
	setUserAdmin(t, db, "admin-overview@example.com")

	w := authGet(t, router, "/api/admin/overview", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var overview map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&overview))
	assert.NotNil(t, overview["system"])
	assert.NotNil(t, overview["ai_today"])
	assert.NotNil(t, overview["effective_models"])

	wH := authGet(t, router, "/api/admin/health", adminToken)
	require.Equal(t, http.StatusOK, wH.Code)
	var health map[string]any
	require.NoError(t, json.NewDecoder(wH.Body).Decode(&health))
	assert.Contains(t, health, "provider")
}

func TestAdmin_LLMUsageEventsEmpty(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := registerAndLogin(t, router, "admin-usage@example.com", "password123")
	setUserAdmin(t, db, "admin-usage@example.com")

	w := authGet(t, router, "/api/admin/llm-usage/events?period=30d", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(0), body["total"])
}

func TestAdmin_PolicyAudit(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := registerAndLogin(t, router, "admin-audit@example.com", "password123")
	setUserAdmin(t, db, "admin-audit@example.com")

	w := authGet(t, router, "/api/admin/models/policy-audit", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
}
