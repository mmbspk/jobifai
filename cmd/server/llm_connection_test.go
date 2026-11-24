package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

func TestLLMConnectionTest_ProductionDependencies(t *testing.T) {
	for _, personal := range []bool{false, true} {
		name := "system provider"
		if personal {
			name = "personal provider with exhausted quota"
		}
		t.Run(name, func(t *testing.T) {
			sqldb, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqldb.Close() })
			cfgStore := config.NewStore(sqldb)
			secrets := config.NewSecretsStore(sqldb, "test-passphrase")
			users := auth.NewUserStore(sqldb)
			quotaSvc := quota.NewService(sqldb, cfgStore, users)
			catalog := pricing.DefaultCatalog()
			quotaSvc.SetPricingCatalog(catalog)
			ledger := &usage.Ledger{DB: sqldb, Catalog: catalog, Quota: quotaSvc, Defaults: quotaSvc.LoadDefaults}
			reuseStore := &llmreuse.Store{DB: sqldb}
			tracker := &llm.UsageTracker{}
			userID := domain.SystemUserID
			checker := noPersonalKey
			key := "system-test-key"
			model := "claude-sonnet-4-6"
			if personal {
				u, err := users.Create("tester@connection.test", "hash", "Tester")
				require.NoError(t, err)
				userID = u.ID
				checker = func(string) (bool, bool) { return false, true }
				key = "personal-test-key"
				model = "claude-haiku-4-5"
				require.NoError(t, quotaSvc.InitTrial(context.Background(), userID))
				_, err = sqldb.Exec(`UPDATE user_quota SET trial_remaining_micro = 0 WHERE user_id = ?`, userID)
				require.NoError(t, err)
				require.NoError(t, cfgStore.Set(userID, config.KeyLLMOverrides, domain.LLMOverrides{Model: model}))
			}

			var providerCalls atomic.Int32
			var rejectKey atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				providerCalls.Add(1)
				assert.Equal(t, key, r.Header.Get("x-api-key"))
				var req struct {
					Model     string `json:"model"`
					MaxTokens int    `json:"max_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				assert.Equal(t, model, req.Model)
				assert.LessOrEqual(t, req.MaxTokens, 32, "connection probes should request only a short reply")
				if rejectKey.Load() {
					http.Error(w, "invalid API key", http.StatusUnauthorized)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model":   model,
					"content": []map[string]string{{"type": "text", "text": "OK"}},
					"usage":   map[string]int{"input_tokens": 10, "output_tokens": 1},
				})
			}))
			t.Cleanup(srv.Close)
			require.NoError(t, cfgStore.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
				LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: true, ProxyURL: srv.URL},
			}))
			require.NoError(t, secrets.Set(domain.SystemUserID, "llm_api_key", "system-test-key"))
			if personal {
				require.NoError(t, secrets.Set(userID, "llm_api_key", key))
			}
			_, _, _, client, isPersonal := buildLLMDeps(userID, checker, cfgStore, secrets, tracker, quotaSvc, ledger, nil, catalog, reuseStore)
			require.NotNil(t, client)
			assert.Equal(t, personal, isPersonal)
			ctx := llm.WithCallContext(context.Background(), domain.LLMCallContext{Task: domain.TaskJobScoring})
			for i := 0; i < 2; i++ {
				probeCtx := context.Background()
				if i > 0 {
					probeCtx = ctx
				}
				provider, gotModel, latency, err := client.TestConnection(probeCtx)
				require.NoError(t, err)
				assert.Equal(t, "claude", provider)
				assert.Equal(t, model, gotModel)
				assert.GreaterOrEqual(t, latency, int64(0))
			}
			assert.EqualValues(t, 2, providerCalls.Load(), "every probe must contact the provider")
			rejectKey.Store(true)
			provider, gotModel, _, err := client.TestConnection(ctx)
			require.ErrorContains(t, err, "llm http 401")
			assert.Equal(t, "claude", provider)
			assert.Equal(t, model, gotModel)
			assert.EqualValues(t, 3, providerCalls.Load())

			for _, table := range []string{"llm_usage_events", "usage_totals", "llm_generation_cache"} {
				var count int
				require.NoError(t, sqldb.QueryRow("SELECT count(*) FROM "+table).Scan(&count))
				assert.Zero(t, count, table)
			}
			assert.Zero(t, tracker.Snapshot().Calls)
			var quotaRows int
			require.NoError(t, sqldb.QueryRow(`SELECT count(*) FROM user_quota WHERE user_id = ?`, domain.SystemUserID).Scan(&quotaRows))
			assert.Zero(t, quotaRows)
			if personal {
				var remaining int64
				require.NoError(t, sqldb.QueryRow(`SELECT trial_remaining_micro FROM user_quota WHERE user_id = ?`, userID).Scan(&remaining))
				assert.Zero(t, remaining)
				_, err = client.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hello"}})
				var exceeded *quota.ExceededError
				require.ErrorAs(t, err, &exceeded, "normal calls must still enforce quota")
				assert.EqualValues(t, 3, providerCalls.Load())
			}
		})
	}
}
