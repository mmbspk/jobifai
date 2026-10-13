package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
)

type generatedContentTailor struct{ countingTailor }

func (c *generatedContentTailor) TailorProfile(ctx context.Context, p *domain.ResumeProfile, job string) (*domain.ResumeProfile, error) {
	c.tailorCalls.Add(1)
	copy := *p
	copy.Summary = "Tailored to this role"
	return &copy, nil
}

func TestGenerate_PersistsOutputAndBillsOnce(t *testing.T) {
	for _, endpoint := range []string{"generate", "generate-tailored", "generate-cover-letter"} {
		t.Run(endpoint, func(t *testing.T) {
			svc, _ := newTestServices(t)
			wireDocumentService(t, svc)
			renderer := &countingRenderer{}
			tailor := &generatedContentTailor{}
			biller := newCountingResourceStub()
			svc.Renderer, svc.Documents.Renderer, svc.Resource = renderer, renderer, biller
			svc.Documents.MarketDir = "../../resume_markets"
			svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) { return nil, tailor }
			router := handler.NewRouter(svc)
			token := registerAndLogin(t, router, endpoint+"-saved@example.com", "password123")
			profile := sampleResumeProfile()
			saveResumeProfile(t, router, token, profile)
			fields := map[string]string{"market": "Australia"}
			if endpoint != "generate" {
				fields["job_description"] = "Looking for an experienced candidate"
			}
			w := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/"+endpoint, token, "save-once", fields)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
			versionID := w.Header().Get("X-Jobifai-Document-Version")
			require.NotEmpty(t, versionID)
			require.Equal(t, int32(1), biller.beforeN.Load())
			require.Equal(t, int32(1), biller.recordN.Load())
			if endpoint == "generate-cover-letter" {
				require.Equal(t, int32(1), tailor.coverCalls.Load())
				require.Equal(t, int32(1), renderer.coverCalls.Load())
			} else {
				require.Equal(t, int32(1), renderer.resumeCalls.Load())
				// Plain Generate doesn't have a market prefix lookup in this fixture.
				wantTailors := int32(0)
				if endpoint == "generate-tailored" {
					wantTailors = 1
				}
				require.Equal(t, wantTailors, tailor.tailorCalls.Load())
			}
			listResp := authGet(t, router, "/api/documents", token)
			require.Equal(t, http.StatusOK, listResp.Code)
			var list documents.ListResponse
			require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &list))
			require.Len(t, list.Documents, 1)
			require.Equal(t, versionID, list.Documents[0].Versions[0].ID)
			require.True(t, list.Documents[0].Versions[0].HasPDF)
			require.True(t, list.Documents[0].Versions[0].Reconstructible)
			require.Empty(t, list.Defaults.ResumeVersionID, "generation must not replace defaults")
			if endpoint == "generate-tailored" {
				detail := authGet(t, router, "/api/documents/versions/"+versionID, token)
				require.Contains(t, detail.Body.String(), "Tailored to this role")
			}
			// Change the live profile and download through the durable Documents API.
			profile.Summary = "Changed after generation"
			saveResumeProfile(t, router, token, profile)
			download := authGet(t, router, "/api/documents/versions/"+versionID+"/pdf", token)
			require.Equal(t, http.StatusOK, download.Code, download.Body.String())
			require.Equal(t, w.Body.Bytes(), download.Body.Bytes())
			require.Equal(t, int32(1), biller.recordN.Load(), "download must not bill or render again")
			require.Equal(t, int32(1), renderer.resumeCalls.Load()+renderer.coverCalls.Load())
		})
	}
}

type failedGenerationRenderer struct{ stubRenderer }

func (failedGenerationRenderer) RenderResume(context.Context, *domain.ResumeProfile, string, string, *resume.RenderOptions) ([]byte, error) {
	return nil, errors.New("renderer unavailable")
}

func TestGenerate_RenderFailureKeepsContentAndDoesNotBill(t *testing.T) {
	router, svc, token := newResumeRouter(t)
	svc.Documents.Renderer = failedGenerationRenderer{}
	biller := newCountingResourceStub()
	svc.Resource = biller
	saveResumeProfile(t, router, token, sampleResumeProfile())
	w := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "Content saved to Documents")
	versionID := w.Header().Get("X-Jobifai-Document-Version")
	require.NotEmpty(t, versionID)
	require.Equal(t, int32(0), biller.recordN.Load())
	detail := authGet(t, router, "/api/documents/versions/"+versionID, token)
	require.Equal(t, http.StatusOK, detail.Code)
}

func TestGenerate_RealLedgerRecordsOneRenderAndDeduplicatesRetry(t *testing.T) {
	svc, _ := newServicesWithResource(t)
	wireDocumentService(t, svc)
	svc.Renderer = stubRenderer{}
	svc.Documents.Renderer = svc.Renderer
	require.NoError(t, svc.Resource.SaveDefaults(domain.ResourceMeteringDefaults{
		Enabled: true,
		Render:  domain.RenderMeteringConfig{Enabled: true, RawCostUSDMicroPerRender: 2_000},
	}))
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "saved-render-ledger@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())
	userID := userIDFromToken(t, router, token)
	for i := 0; i < 2; i++ {
		w := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, "same-render-operation", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		download := authGet(t, router, "/api/documents/versions/"+w.Header().Get("X-Jobifai-Document-Version")+"/pdf", token)
		require.Equal(t, http.StatusOK, download.Code)
	}
	var events, credits int
	require.NoError(t, svc.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(credits_burned), 0) FROM resource_usage_events WHERE user_id=?`, userID).Scan(&events, &credits))
	require.Equal(t, 1, events, "saving must not add a second render event")
	require.Equal(t, 3, credits, "retry uses the existing user/endpoint-scoped billing key")
}

func TestDocuments_AppendEditedResumePreservesOriginalVersion(t *testing.T) {
	router, _, token := newResumeRouter(t)
	profile := sampleResumeProfile()
	saveResumeProfile(t, router, token, profile)
	created := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	originalID := created.Header().Get("X-Jobifai-Document-Version")
	original := authGet(t, router, "/api/documents/versions/"+originalID, token)
	var detail documents.VersionDetail
	require.NoError(t, json.Unmarshal(original.Body.Bytes(), &detail))
	profile.Skills = []string{"Leadership", "Planning"}
	profile.ExperienceDetails = []domain.ExperienceDetail{{Company: "Acme", KeyResponsibilities: []string{"Reduced costs"}}}
	w := authPost(t, router, "/api/documents/"+detail.DocumentID+"/resume-versions", token, map[string]any{"profile": profile})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var result map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.NotEqual(t, originalID, result["content_version_id"])
	updated := authGet(t, router, "/api/documents/versions/"+result["content_version_id"], token)
	require.Contains(t, updated.Body.String(), "Leadership")
	require.Contains(t, updated.Body.String(), "Reduced costs")
	reloaded := authGet(t, router, "/api/documents/versions/"+originalID, token)
	require.JSONEq(t, original.Body.String(), reloaded.Body.String())
}
