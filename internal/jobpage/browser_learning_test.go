package jobpage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the full live DOM → configured HTTP client → independent verifier
// → durable recipe → fresh replay chain. Only the provider is a local fixture.
func TestBrowserLearningColdWarmAndChangedLayoutAccounting(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		t.Run(platform, func(t *testing.T) {
			browser := readerFixtureBrowser(t)
			store, _ := recipeStoreFixture(t)
			model, calls := modelFixture(t, nil, func(w http.ResponseWriter, r *http.Request, index int) {
				// Decode the provider message envelope without assuming image/text transport.
				var envelope struct {
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", 400)
					return
				}
				observed := ""
				for _, message := range envelope.Messages {
					var text string
					if json.Unmarshal(message.Content, &text) == nil {
						observed += text
						continue
					}
					var parts []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					}
					if err := json.Unmarshal(message.Content, &parts); err != nil {
						t.Error(err)
						http.Error(w, "invalid content", 400)
						return
					}
					for _, part := range parts {
						if part.Type == "text" {
							observed += part.Text
						}
					}
				}
				marker := "OBSERVED_SNAPSHOT:\n"
				start := strings.Index(observed, marker)
				if start < 0 {
					t.Error("owned snapshot missing")
					http.Error(w, "snapshot missing", 400)
					return
				}
				raw := strings.Split(observed[start+len(marker):], "\nPROPOSAL_TO_VERIFY:\n")[0]
				var snapshot Snapshot
				if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
					t.Error(err)
					http.Error(w, "snapshot invalid", 400)
					return
				}
				if index%2 == 0 {
					plan := Plan{Version: SnapshotVersion, SnapshotID: snapshot.ID, DescriptionRefs: snapshot.Candidates}
					text, _ := json.Marshal(plan)
					modelReply(w, string(text), "claude-haiku-4-5", 10, 5)
				} else {
					verdict := Verification{SnapshotID: snapshot.ID}
					for _, ref := range snapshot.Candidates {
						verdict.Verdicts = append(verdict.Verdicts, Verdict{Ref: ref, IsDescription: true, Supported: true, Complete: true})
					}
					text, _ := json.Marshal(verdict)
					modelReply(w, string(text), "claude-haiku-4-5", 10, 5)
				}
			})
			reader := NewReader(store, enabledReaderConfig, func(_ context.Context, limits Limits) (Model, error) { return model.NewSession(limits), nil })
			fixture := func(id, tag, description string) (*BrowserPage, Target) {
				url := "https://www.linkedin.com/jobs/view/" + id
				header := fmt.Sprintf("<main data-job-id='%s'><h1>Engineer</h1><a aria-label='Company, Acme.'>Acme</a><button onclick='window.unexpectedApply=true'>Apply</button>", id)
				if platform == "seek" {
					url = "https://www.seek.com.au/job/" + id
					header = fmt.Sprintf("<main><h1 data-automation='job-detail-title'>Engineer</h1><a data-automation='advertiser-name'>Acme</a><a data-automation='job-detail-apply' href='/job/%s/apply' onclick='window.unexpectedApply=true;return false'>Apply</a>", id)
				}
				target, _, err := NormalizeTarget(Target{Platform: platform, URL: url, Title: "Engineer", Company: "Acme", UserID: "reader-user"})
				require.NoError(t, err)
				html := header + fmt.Sprintf("<%s data-testid='role-copy'><h2>Responsibilities</h2><p>%s</p></%s></main>", tag, description, tag)
				page, _ := readerFixture(t, browser, target, html)
				return page, target
			}
			coldPage, coldTarget := fixture("123", "section", strings.Repeat("Build reliable services and review production deployments. ", 8))
			cold, err := reader.Read(t.Context(), coldPage, coldTarget)
			require.NoError(t, err)
			require.Equal(t, SourceModel, cold.Source)
			require.NotEmpty(t, cold.RecipeID)
			require.EqualValues(t, 2, calls.Load())
			require.Equal(t, 1, cold.Metrics.ProposalRequests)
			require.Equal(t, 1, cold.Metrics.VerificationRequests)
			require.Equal(t, 2, cold.Metrics.HTTPAttempts)
			require.EqualValues(t, 20, cold.Metrics.InputTokens)
			require.EqualValues(t, 10, cold.Metrics.OutputTokens)
			require.True(t, cold.Metrics.CostKnown)
			require.Positive(t, cold.Metrics.CostMicroUSD)
			require.Positive(t, cold.Metrics.BrowserMS)
			warmText := strings.Repeat("Current employer needs platform automation and incident response. ", 8)
			warmPage, warmTarget := fixture("456", "section", warmText)
			warm, err := reader.Read(t.Context(), warmPage, warmTarget)
			require.NoError(t, err)
			require.Equal(t, SourceRecipe, warm.Source)
			require.Equal(t, cold.RecipeID, warm.RecipeID)
			require.EqualValues(t, 2, calls.Load())
			require.Zero(t, warm.Metrics.HTTPAttempts)
			require.Contains(t, warm.Description, "Current employer")
			require.NotContains(t, warm.Description, "Build reliable services")
			changedPage, changedTarget := fixture("789", "article", strings.Repeat("Changed layout contains this role's current requirements. ", 8))
			changed, err := reader.Read(t.Context(), changedPage, changedTarget)
			require.NoError(t, err)
			require.Equal(t, SourceModel, changed.Source)
			require.NotEqual(t, cold.RecipeID, changed.RecipeID)
			require.EqualValues(t, 4, calls.Load())
			for _, page := range []*BrowserPage{coldPage, warmPage, changedPage} {
				value, err := page.page.Eval("()=>window.unexpectedApply===true")
				require.NoError(t, err)
				require.False(t, value.Value.Bool(), "reading ownership must never grant application input")
			}
			for _, result := range []Result{cold, warm, changed} {
				t.Logf("platform=%s source=%s requests=%d tokens=%d/%d cost_micro_usd=%d browser_ms=%d latency_ms=%d", platform, result.Source, result.Metrics.HTTPAttempts, result.Metrics.InputTokens, result.Metrics.OutputTokens, result.Metrics.CostMicroUSD, result.Metrics.BrowserMS, result.Metrics.LatencyMS)
			}
		})
	}
}
