package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/retention"
)

func TestAdmin_RetentionDefaults_AuditAndClamp(t *testing.T) {
	t.Parallel()
	svc, db := newTestServices(t)
	svc.Retention = &retention.Service{DB: db, Config: svc.Config, Root: t.TempDir()}
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-retention@example.com")

	wGet := authGet(t, router, "/api/admin/retention/defaults", adminToken)
	require.Equal(t, http.StatusOK, wGet.Code)
	var defaults domain.DocumentRetentionDefaults
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&defaults))
	assert.Equal(t, domain.DocumentRetentionDefault, defaults.LatestSubmittedApplications)

	wPut := authPut(t, router, "/api/admin/retention/defaults", adminToken, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 5})
	require.Equal(t, http.StatusOK, wPut.Code)
	require.NoError(t, json.NewDecoder(wPut.Body).Decode(&defaults))
	assert.Equal(t, 5, defaults.LatestSubmittedApplications)

	wPut30 := authPut(t, router, "/api/admin/retention/defaults", adminToken, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 99})
	require.Equal(t, http.StatusOK, wPut30.Code)
	require.NoError(t, json.NewDecoder(wPut30.Body).Decode(&defaults))
	assert.Equal(t, domain.DocumentRetentionMax, defaults.LatestSubmittedApplications)

	wAudit := authGet(t, router, "/api/admin/retention/audit?limit=10", adminToken)
	require.Equal(t, http.StatusOK, wAudit.Code)
	var audit []retention.AuditRow
	require.NoError(t, json.NewDecoder(wAudit.Body).Decode(&audit))
	require.GreaterOrEqual(t, len(audit), 2)
}
