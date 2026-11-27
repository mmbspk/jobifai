package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

// Use the server-assembled reader factory, authenticated browser registry, and
// Generate bridge. The loopback provider is the only substituted service.
func TestJobPageManagerLearningAndCancellationBothBoards(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		t.Run(platform, func(t *testing.T) {
			db := pageHookDB(t)
			m := pageHookManager(t, db)
			var calls atomic.Int32
			var cancelProvider atomic.Bool
			providerEntered := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := calls.Add(1) - 1
				if cancelProvider.Load() {
					providerEntered <- struct{}{}
					select {
					case <-r.Context().Done():
					case <-t.Context().Done():
					}
					return
				}
				var envelope struct {
					Model    string `json:"model"`
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", 400)
					return
				}
				if envelope.Model != "claude-haiku-4-5" || r.Header.Get("x-api-key") != "fixture-system-key" {
					t.Error("reader bypassed approved policy or deployment credential")
					http.Error(w, "incorrect routing", 400)
					return
				}
				var observed strings.Builder
				for _, message := range envelope.Messages {
					var text string
					if json.Unmarshal(message.Content, &text) == nil {
						observed.WriteString(text)
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
							observed.WriteString(part.Text)
						}
					}
				}
				_, raw, found := strings.Cut(observed.String(), "OBSERVED_SNAPSHOT:\n")
				raw, _, _ = strings.Cut(raw, "\nPROPOSAL_TO_VERIFY:\n")
				var snapshot jobpage.Snapshot
				if !found || json.Unmarshal([]byte(raw), &snapshot) != nil {
					t.Error("owned snapshot missing or invalid")
					http.Error(w, "snapshot missing", 400)
					return
				}
				var reply any = jobpage.Plan{Version: jobpage.SnapshotVersion, SnapshotID: snapshot.ID, DescriptionRefs: snapshot.Candidates}
				if index%2 == 1 {
					verdict := jobpage.Verification{SnapshotID: snapshot.ID}
					for _, ref := range snapshot.Candidates {
						verdict.Verdicts = append(verdict.Verdicts, jobpage.Verdict{Ref: ref, IsDescription: true, Supported: true, Complete: true})
					}
					reply = verdict
				}
				text, err := json.Marshal(reply)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"model": "claude-haiku-4-5", "content": []map[string]any{{"type": "text", "text": string(text)}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}}); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(srv.Close)
			settings := m.cfgStore.(*config.Store)
			secrets := m.secrets.(*config.SecretsStore)
			require.NoError(t, settings.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: true, ProxyURL: srv.URL}}))
			require.NoError(t, secrets.Set(domain.SystemUserID, "proxy_key", "fixture-system-key"))
			// An admin with no selected personal proxy key must revert this stale URL
			// and unrelated personal direct key to the deployment configuration.
			m.SetUserTypeChecker(func(string) (bool, bool) { return true, false })
			require.NoError(t, settings.Set(pageTestUser, config.KeyGeneralSettings, domain.GeneralSettings{LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: true, ProxyURL: "http://127.0.0.1:1/stale"}}))
			require.NoError(t, secrets.Set(pageTestUser, "llm_api_key", "unrelated-personal-key"))
			_, err := db.Exec("INSERT INTO task_model_policies(task,state,provider,model,fallback_models,mode,max_tokens) VALUES(?,'approved','claude','claude-haiku-4-5','[]','default',4000)", domain.TaskJobPageReading)
			require.NoError(t, err)
			m.SetLLMBilling(&llmpolicy.Store{DB: db}, pricing.DefaultCatalog(), nil)
			cfg, err := m.pageStore.GetConfig(t.Context())
			require.NoError(t, err)
			cfg.Recovery, cfg.Reuse = jobpage.ModePilot, jobpage.ModePilot
			cfg.PilotUsers = []string{pageTestUser}
			cfg.Limits.MaxRequests = 2
			_, err = m.pageStore.SetConfig(t.Context(), pageTestUser, cfg.Revision, cfg)
			require.NoError(t, err)
			urlFor := func(id string) string {
				if platform == "seek" {
					return "https://www.seek.com.au/job/" + id
				}
				return "https://www.linkedin.com/jobs/view/" + id
			}
			br := workerFixtureBrowser(t, func(raw string) string {
				id := linkedInExpectedID(raw)
				if platform == "seek" {
					id = seekJobIDFromURL(raw)
				}
				tag, description := "section", strings.Repeat("Cold listing requires reliable services and careful deployments. ", 8)
				switch id {
				case "22345679":
					description = strings.Repeat("Current warm employer needs incident response and automation. ", 8)
				case "22345680":
					tag, description = "article", strings.Repeat("Changed layout needs infrastructure design and reviews. ", 8)
				case "22345681":
					tag = "div"
				}
				html := pageHookHTML(platform, id, "")
				html = strings.Replace(html, "</main>", fmt.Sprintf("<%s data-testid='role-copy'><h2>Responsibilities</h2><p>%s</p></%s></main>", tag, description, tag), 1)
				return strings.Replace(html, "sessionStorage", "localStorage", 1)
			})
			if platform == "seek" {
				m.seekBrowsers[pageTestUser] = br
			} else {
				m.linkedInBrowsers[pageTestUser] = br
			}
			cold, err := m.ReadJobPage(t.Context(), pageTestUser, urlFor("22345678"))
			require.NoError(t, err)
			require.Equal(t, jobpage.SourceModel, cold.Source)
			require.NotEmpty(t, cold.RecipeID)
			require.EqualValues(t, 2, calls.Load())
			require.Equal(t, 2, cold.Metrics.HTTPAttempts)
			warm, err := m.FetchJobPage(t.Context(), pageTestUser, urlFor("22345679"))
			require.NoError(t, err)
			require.Contains(t, warm, "Job title: Engineer\nCompany: Acme\n\n")
			require.Contains(t, warm, "Current warm employer")
			require.NotContains(t, warm, "Cold listing")
			require.EqualValues(t, 2, calls.Load())
			changed, err := m.ReadJobPage(t.Context(), pageTestUser, urlFor("22345680"))
			require.NoError(t, err)
			require.Equal(t, jobpage.SourceModel, changed.Source)
			require.NotEqual(t, cold.RecipeID, changed.RecipeID)
			require.Contains(t, changed.Description, "Changed layout")
			require.EqualValues(t, 4, calls.Load())
			cancelProvider.Store(true)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := m.FetchJobPage(ctx, pageTestUser, urlFor("22345681")); done <- err }()
			select {
			case <-providerEntered:
			case <-time.After(20 * time.Second):
				t.Fatal("caller did not enter cold provider request")
			}
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, jobpage.FailureCanceled, jobpage.ErrorCode(err))
			case <-time.After(5 * time.Second):
				t.Fatal("caller failed to cancel in-flight provider request")
			}
			require.EqualValues(t, 5, calls.Load(), "cancellation must not trigger another request")
			events, err := m.pageStore.ListReadEvents(t.Context(), pageTestUser, 10)
			require.NoError(t, err)
			require.Len(t, events, 4)
			require.Equal(t, jobpage.FailureCanceled, events[0].Failure)
			require.Empty(t, events[0].RecipeID)
			require.Equal(t, jobpage.SourceRecipe, events[2].Source)
			require.Equal(t, cold.RecipeID, events[2].RecipeID)
			require.Zero(t, events[2].Metrics.HTTPAttempts)
			var recipes, applications int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM job_page_recipes").Scan(&recipes))
			require.Equal(t, 2, recipes, "canceled content must never become a recipe")
			require.NoError(t, db.QueryRow("SELECT count(*) FROM jobs_applied").Scan(&applications))
			require.Zero(t, applications)
			probe := navigateAndWait(t, br, urlFor("22345678"))
			clicked, err := probe.Eval("()=>localStorage.getItem('read-only-apply-click')==='true'")
			require.NoError(t, err)
			require.False(t, clicked.Value.Bool())
		})
	}
}
