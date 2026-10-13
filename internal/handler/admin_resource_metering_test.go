package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resource"
)

// stubResourceService implements handler.ResourceService for handler tests.
type stubResourceService struct {
	defaults         domain.ResourceMeteringDefaults
	loadErr          error
	saveErr          error
	renderCredits    int64
	browserCredits   int64
}

func (s *stubResourceService) LoadDefaults() (domain.ResourceMeteringDefaults, error) {
	return s.defaults, s.loadErr
}
func (s *stubResourceService) SaveDefaults(d domain.ResourceMeteringDefaults) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.defaults = d
	return nil
}
func (s *stubResourceService) EstimateRenderCredits() (int64, error)    { return s.renderCredits, nil }
func (s *stubResourceService) EstimateBrowserIncrementCredits() (int64, error) { return s.browserCredits, nil }
func (s *stubResourceService) BeforeRender(_ context.Context, _ string) error   { return nil }
func (s *stubResourceService) RecordRender(_ context.Context, _, _ string) error { return nil }

// newServicesWithResource returns a Services bag with a real resource.Service attached.
func newServicesWithResource(t *testing.T) (*handler.Services, func() string) {
	t.Helper()
	svc, db := newTestServices(t)
	cfg := config.NewStore(db)
	require.NoError(t, cfg.Set(domain.SystemUserID, "quota_defaults", domain.QuotaDefaults{
		EnforcementDefault: true, CreditsPerUSD: 1000, ServiceMarkup: 0.5,
		TrialCredits: 500, TrialDays: 7,
	}))
	quotaSvc := quota.NewService(db, cfg, nil)
	resSvc := resource.NewService(db, cfg, quotaSvc, quotaSvc.LoadDefaults)
	svc.Quota = quotaSvc
	svc.Resource = resSvc

	router := handler.NewRouter(svc)
	adminEmail := "admin-rm@example.com"
	tok := registerAndLogin(t, router, adminEmail, "admin-rm-pass")
	setUserAdmin(t, db, adminEmail)
	return svc, func() string { return tok }
}

// TestResourceMeteringGet_Admin verifies the GET endpoint returns 200 with defaults for admins.
func TestResourceMeteringGet_Admin(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/admin/resource-metering/defaults", getToken())
	assert.Equal(t, 200, w.Code, "GET must return 200 for admin: %s", w.Body)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp, "enabled")
	assert.Contains(t, resp, "browser_automation")
	assert.Contains(t, resp, "document_render")
	assert.Contains(t, resp, "per_call_fee_is_ai_only")
	assert.True(t, resp["per_call_fee_is_ai_only"].(bool))
}

// TestResourceMeteringGet_NonAdmin verifies non-admin users receive 403.
func TestResourceMeteringGet_NonAdmin(t *testing.T) {
	svc, _ := newServicesWithResource(t)
	router := handler.NewRouter(svc)
	// Register a plain user (non-admin).
	userToken := registerAndLogin(t, router, "plain-rm@example.com", "pass1234")

	w := authGet(t, router, "/api/admin/resource-metering/defaults", userToken)
	assert.Equal(t, 403, w.Code, "non-admin must get 403")
}

// TestResourceMeteringPut_ValidConfig verifies saving a valid configuration returns 200.
func TestResourceMeteringPut_ValidConfig(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)

	body := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 20_000, BillingIncrementSecs: 30},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}
	w := authPut(t, router, "/api/admin/resource-metering/defaults", getToken(), body)
	assert.Equal(t, 200, w.Code, "PUT valid config must return 200: %s", w.Body)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp["enabled"].(bool), "enabled must be true in response")
}

// TestResourceMeteringPut_InvalidInterval verifies an out-of-range interval returns 400.
func TestResourceMeteringPut_InvalidInterval(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)

	body := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 3}, // invalid: < 5
	}
	w := authPut(t, router, "/api/admin/resource-metering/defaults", getToken(), body)
	assert.Equal(t, 400, w.Code, "invalid interval must return 400")

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["message"].(string), "billing_increment_seconds")
}

// TestResourceMeteringPut_NegativeRates verifies negative rates return 400.
func TestResourceMeteringPut_NegativeRates(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)

	body := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{RawCostUSDMicroPerHour: -1, BillingIncrementSecs: 30},
	}
	w := authPut(t, router, "/api/admin/resource-metering/defaults", getToken(), body)
	assert.Equal(t, 400, w.Code, "negative browser rate must return 400: %s", w.Body)
}

// TestResourceMeteringPut_NegativeRenderRate verifies negative render rate returns 400.
func TestResourceMeteringPut_NegativeRenderRate(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)

	body := domain.ResourceMeteringDefaults{
		Render: domain.RenderMeteringConfig{RawCostUSDMicroPerRender: -1},
	}
	w := authPut(t, router, "/api/admin/resource-metering/defaults", getToken(), body)
	assert.Equal(t, 400, w.Code, "negative render rate must return 400: %s", w.Body)
}

// TestResourceMeteringPersistenceRoundtrip verifies PUT then GET returns the saved values.
func TestResourceMeteringPersistenceRoundtrip(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)
	tok := getToken()

	saved := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 50_000, BillingIncrementSecs: 60},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 3_000},
	}
	wPut := authPut(t, router, "/api/admin/resource-metering/defaults", tok, saved)
	require.Equal(t, 200, wPut.Code, "PUT must succeed: %s", wPut.Body)

	wGet := authGet(t, router, "/api/admin/resource-metering/defaults", tok)
	require.Equal(t, 200, wGet.Code)

	var resp struct {
		Enabled bool `json:"enabled"`
		Browser struct {
			RawCostUSDMicroPerHour int64 `json:"raw_cost_usd_micro_per_hour"`
			BillingIncrementSecs   int   `json:"billing_increment_seconds"`
		} `json:"browser_automation"`
		Render struct {
			RawCostUSDMicroPerRender int64 `json:"raw_cost_usd_micro_per_render"`
		} `json:"document_render"`
	}
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, int64(50_000), resp.Browser.RawCostUSDMicroPerHour)
	assert.Equal(t, 60, resp.Browser.BillingIncrementSecs)
	assert.Equal(t, int64(3_000), resp.Render.RawCostUSDMicroPerRender)
}

// TestResourceMeteringCreditDisplayValues verifies the computed credit estimates in the response.
// With render=2000 micro-USD, 50% markup, 1000 c/$:
//   render_credits_per_render = 3
//   browser 60s: 50000×60/3600=833 micro-USD raw; loaded=1249; credits=ceil(1.249)=2
//   browser_credits_per_minute = 2 credits/increment × (60s/60s) = 2.0
func TestResourceMeteringCreditDisplayValues(t *testing.T) {
	svc, getToken := newServicesWithResource(t)
	router := handler.NewRouter(svc)
	tok := getToken()

	body := domain.ResourceMeteringDefaults{
		Enabled: true,
		Browser: domain.BrowserMeteringConfig{Enabled: true, RawCostUSDMicroPerHour: 50_000, BillingIncrementSecs: 60},
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}
	wPut := authPut(t, router, "/api/admin/resource-metering/defaults", tok, body)
	require.Equal(t, 200, wPut.Code)

	var resp struct {
		RenderCreditsPerRender  int64   `json:"render_credits_per_render"`
		BrowserCreditsPerMinute float64 `json:"browser_credits_per_minute"`
	}
	require.NoError(t, json.NewDecoder(wPut.Body).Decode(&resp))
	assert.Equal(t, int64(3), resp.RenderCreditsPerRender, "render credits: 2000µ-USD×1.5×1000/1M=3")
	assert.Greater(t, resp.BrowserCreditsPerMinute, float64(0), "browser credits/min must be positive")
}

// TestResourceMeteringGet_ConfigError returns 500 when the storage layer errors (F6).
func TestResourceMeteringGet_ConfigError(t *testing.T) {
	svc, db := newTestServices(t)
	svc.Resource = &stubResourceService{
		loadErr: errors.New("simulated storage failure"),
	}
	router := handler.NewRouter(svc)
	adminEmail := "admin-err@example.com"
	tok := registerAndLogin(t, router, adminEmail, "pass1234")
	setUserAdmin(t, db, adminEmail)

	w := authGet(t, router, "/api/admin/resource-metering/defaults", tok)
	assert.Equal(t, 500, w.Code, "config read error must return 500, not 200 with fallback: %s", w.Body)
}

// TestResourceMeteringPut_InfraError returns 500 when SaveDefaults fails with a non-validation error.
func TestResourceMeteringPut_InfraError(t *testing.T) {
	svc, db := newTestServices(t)
	svc.Resource = &stubResourceService{
		defaults: domain.ResourceMeteringDefaults{},
		saveErr:  errors.New("simulated infra failure"),
	}
	router := handler.NewRouter(svc)
	adminEmail := "admin-infra@example.com"
	tok := registerAndLogin(t, router, adminEmail, "pass1234")
	setUserAdmin(t, db, adminEmail)

	body := domain.ResourceMeteringDefaults{
		Browser: domain.BrowserMeteringConfig{BillingIncrementSecs: 30},
	}
	w := authPut(t, router, "/api/admin/resource-metering/defaults", tok, body)
	assert.Equal(t, 500, w.Code, "infra save error must return 500: %s", w.Body)
}
