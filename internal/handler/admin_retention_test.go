package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
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

	wPutBad := authPut(t, router, "/api/admin/retention/defaults", adminToken, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 99})
	require.Equal(t, http.StatusBadRequest, wPutBad.Code)

	wPut30 := authPut(t, router, "/api/admin/retention/defaults", adminToken, domain.DocumentRetentionDefaults{LatestSubmittedApplications: 30})
	require.Equal(t, http.StatusOK, wPut30.Code)
	require.NoError(t, json.NewDecoder(wPut30.Body).Decode(&defaults))
	assert.Equal(t, 30, defaults.LatestSubmittedApplications)

	wAudit := authGet(t, router, "/api/admin/retention/audit?limit=10", adminToken)
	require.Equal(t, http.StatusOK, wAudit.Code)
	var audit []retention.AuditRow
	require.NoError(t, json.NewDecoder(wAudit.Body).Decode(&audit))
	require.GreaterOrEqual(t, len(audit), 2)
}

func TestAdmin_DocumentsArtifactMetrics_PopulatedTotals(t *testing.T) {
	t.Parallel()
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-metrics-pop@example.com")

	// Retrieve the admin user ID so the FK chain is valid.
	userID := userIDFromToken(t, router, adminToken)

	// Seed a document → version → artifact chain so the retained-artifact query
	// returns a non-zero count and byte total.
	docID := "doc-metrics-1"
	verID := "ver-metrics-1"
	artID := "art-metrics-1"
	_, err := db.Exec(`INSERT INTO user_documents (id, user_id, kind, title) VALUES (?, ?, 'resume', 'Test')`,
		docID, userID)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO document_content_versions
			(id, document_id, user_id, version_number, source, content_kind, content_json)
		VALUES (?, ?, ?, 1, 'test', 'resume_json', '{}')`,
		verID, docID, userID)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO document_render_artifacts
			(id, user_id, content_version_id, storage_key, sha256, byte_size, renderer_version, template_identity, state)
		VALUES (?, ?, ?, 'blobs/k1', 'sha-art-1', 54321, 'v1', 'tpl', 'ready')`,
		artID, userID, verID)
	require.NoError(t, err)

	w := authGet(t, router, "/api/admin/documents/artifact-metrics", adminToken)
	require.Equal(t, http.StatusOK, w.Code)

	var snap documents.DocumentsMetricsSnapshot
	require.NoError(t, json.NewDecoder(w.Body).Decode(&snap))
	assert.Equal(t, int64(1), snap.RetainedArtifacts, "retained artifact count must reflect seeded row")
	assert.Equal(t, int64(54321), snap.RetainedArtifactBytes, "retained artifact bytes must reflect seeded row")

	// Evict the artifact and confirm the gauge drops to zero.
	_, err = db.Exec(`UPDATE document_render_artifacts SET evicted_at = datetime('now') WHERE id = ?`, artID)
	require.NoError(t, err)
	w2 := authGet(t, router, "/api/admin/documents/artifact-metrics", adminToken)
	require.Equal(t, http.StatusOK, w2.Code)
	var snap2 documents.DocumentsMetricsSnapshot
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&snap2))
	assert.Equal(t, int64(0), snap2.RetainedArtifacts, "evicted artifact must not appear in retained count")
	assert.Equal(t, int64(0), snap2.RetainedArtifactBytes)
}

func TestAdmin_DocumentsArtifactMetrics_DBQueryError(t *testing.T) {
	t.Parallel()
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, fmt.Sprintf("admin-metrics-err@example.com"))

	// Drop the artifact table to force a SQL error in the retained-artifact query.
	// The users table is untouched so auth middleware succeeds.
	_, err := db.Exec(`DROP TABLE document_render_artifacts`)
	require.NoError(t, err)

	w := authGet(t, router, "/api/admin/documents/artifact-metrics", adminToken)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.NotEmpty(t, body["message"])
}
