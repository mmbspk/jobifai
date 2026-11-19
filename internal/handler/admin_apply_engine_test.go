package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// TestApplyEngine_DefaultIsLegacy verifies GET returns legacy when no config has been stored.
func TestApplyEngine_DefaultIsLegacy(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine1@example.com", "password123")
	setUserAdmin(t, db, "admin-engine1@example.com")

	w := authGet(t, router, "/api/admin/apply-engine", token)
	require.Equal(t, http.StatusOK, w.Code)

	var got domain.ApplyEngineConfig
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, domain.ApplyEngineLegacy, got.Mode)
	assert.False(t, got.ExternalATSEnabled, "external ATS must be off by default")
}

// TestApplyEngine_RoundTrip verifies a PUT followed by GET returns the saved config.
func TestApplyEngine_RoundTrip(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine2@example.com", "password123")
	setUserAdmin(t, db, "admin-engine2@example.com")

	cfg := domain.ApplyEngineConfig{
		Mode:               domain.ApplyEngineShadow,
		ExternalATSEnabled: false,
		PilotUserIDs:       []string{"user-a", "user-b"},
	}
	wPut := authPut(t, router, "/api/admin/apply-engine", token, cfg)
	require.Equal(t, http.StatusOK, wPut.Code)

	var putResp map[string]any
	require.NoError(t, json.NewDecoder(wPut.Body).Decode(&putResp))
	assert.Equal(t, "shadow", putResp["mode"])

	wGet := authGet(t, router, "/api/admin/apply-engine", token)
	require.Equal(t, http.StatusOK, wGet.Code)
	var got domain.ApplyEngineConfig
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	assert.Equal(t, domain.ApplyEngineShadow, got.Mode)
	assert.Equal(t, []string{"user-a", "user-b"}, got.PilotUserIDs)
}

// TestApplyEngine_RejectsInvalidMode verifies an unknown mode is rejected.
func TestApplyEngine_RejectsInvalidMode(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine3@example.com", "password123")
	setUserAdmin(t, db, "admin-engine3@example.com")

	wPut := authPut(t, router, "/api/admin/apply-engine", token, map[string]any{"mode": "turbo"})
	assert.Equal(t, http.StatusBadRequest, wPut.Code)
}

// TestApplyEngine_UnauthorizedWrite verifies non-admins cannot change engine config.
func TestApplyEngine_UnauthorizedWrite(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "user-engine@example.com", "password123")

	wPut := authPut(t, router, "/api/admin/apply-engine", token, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive})
	assert.Equal(t, http.StatusForbidden, wPut.Code)
}

// TestApplyEngine_AuditRecorded verifies a PUT writes an audit entry.
func TestApplyEngine_AuditRecorded(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine4@example.com", "password123")
	setUserAdmin(t, db, "admin-engine4@example.com")

	wPut := authPut(t, router, "/api/admin/apply-engine?reason=smoke-test", token, domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow})
	require.Equal(t, http.StatusOK, wPut.Code)

	wAudit := authGet(t, router, "/api/admin/apply-engine/audit", token)
	require.Equal(t, http.StatusOK, wAudit.Code)

	var resp struct {
		Entries []struct {
			OldMode string `json:"old_mode"`
			NewMode string `json:"new_mode"`
			Reason  string `json:"reason"`
		} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(wAudit.Body).Decode(&resp))
	require.NotEmpty(t, resp.Entries)
	assert.Equal(t, "legacy", resp.Entries[0].OldMode)
	assert.Equal(t, "shadow", resp.Entries[0].NewMode)
	assert.Equal(t, "smoke-test", resp.Entries[0].Reason)
}

// TestApplyEngine_RollbackToLegacy verifies toggling from Shadow back to Legacy
// requires no deployment (config-only change).
func TestApplyEngine_RollbackToLegacy(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine5@example.com", "password123")
	setUserAdmin(t, db, "admin-engine5@example.com")

	// Set to Shadow.
	wShadow := authPut(t, router, "/api/admin/apply-engine", token, domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow})
	require.Equal(t, http.StatusOK, wShadow.Code)

	// Roll back to Legacy.
	wLegacy := authPut(t, router, "/api/admin/apply-engine?reason=rollback", token, domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy})
	require.Equal(t, http.StatusOK, wLegacy.Code)

	// Verify current mode is Legacy.
	wGet := authGet(t, router, "/api/admin/apply-engine", token)
	var cfg domain.ApplyEngineConfig
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&cfg))
	assert.Equal(t, domain.ApplyEngineLegacy, cfg.Mode)

	// Audit should have two entries: shadow then legacy.
	wAudit := authGet(t, router, "/api/admin/apply-engine/audit", token)
	var audit struct {
		Entries []struct {
			NewMode string `json:"new_mode"`
			Reason  string `json:"reason"`
		} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(wAudit.Body).Decode(&audit))
	require.GreaterOrEqual(t, len(audit.Entries), 2, "expected at least two audit entries")
	assert.Equal(t, "legacy", audit.Entries[0].NewMode)
	assert.Equal(t, "rollback", audit.Entries[0].Reason)
}

// TestApplyEngine_ExternalATSDefaultOff verifies the external ATS flag starts off.
func TestApplyEngine_ExternalATSDefaultOff(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine6@example.com", "password123")
	setUserAdmin(t, db, "admin-engine6@example.com")

	wGet := authGet(t, router, "/api/admin/apply-engine", token)
	var cfg domain.ApplyEngineConfig
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&cfg))
	assert.False(t, cfg.ExternalATSEnabled)
}

// TestApplyEngine_PlatformOverridesRoundTrip verifies per-platform overrides persist.
func TestApplyEngine_PlatformOverridesRoundTrip(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "admin-engine7@example.com", "password123")
	setUserAdmin(t, db, "admin-engine7@example.com")

	cfg := domain.ApplyEngineConfig{
		Mode: domain.ApplyEngineLegacy,
		PlatformOverrides: map[string]domain.ApplyEngineMode{
			"linkedin": domain.ApplyEngineShadow,
		},
	}
	wPut := authPut(t, router, "/api/admin/apply-engine", token, cfg)
	require.Equal(t, http.StatusOK, wPut.Code)

	wGet := authGet(t, router, "/api/admin/apply-engine", token)
	var got domain.ApplyEngineConfig
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&got))
	require.NotNil(t, got.PlatformOverrides)
	assert.Equal(t, domain.ApplyEngineShadow, got.PlatformOverrides["linkedin"])
}
