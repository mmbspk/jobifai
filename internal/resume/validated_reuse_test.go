package resume_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
	"github.com/user/jobifai/internal/resume"
)

func TestTailor_RejectsEmptyDocumentsBeforeCaching(t *testing.T) {
	for _, cover := range []bool{false, true} {
		name := "resume"
		responses := []string{"null", "{}", `{"summary":"Experienced candidate"}`}
		if cover {
			name = "cover"
			responses = []string{"null", `{"body":"not prose"}`, "Dear hiring team, I would welcome an opportunity to discuss this role."}
		}
		t.Run(name, func(t *testing.T) {
			db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			// Migration 039 adds FK user_id → users on llm_generation_cache.
			_, err = db.Exec(`INSERT INTO users (id, email, password_hash, is_admin, created_at)
				VALUES ('u', 'u@reuse.test', 'hash', 0, datetime('now'))`)
			require.NoError(t, err)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(calls.Add(1)) - 1
				if i >= len(responses) {
					i = len(responses) - 1
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"text": responses[i]}}, "usage": map[string]int{}})
			}))
			defer srv.Close()
			c := llm.New(domain.LLMConfig{Provider: "claude", Model: "mock", UseProxy: true, ProxyURL: srv.URL}, "k").WithUserID("u").WithReuse(&llm.ReuseCoordinator{Store: &llmreuse.Store{DB: db}})
			tailor := resume.NewTailor(c, c, c, c)
			call := func() error {
				if cover {
					_, err := tailor.WriteCoverLetter(context.Background(), &domain.ResumeProfile{}, "role", "")
					return err
				}
				_, err := tailor.TailorProfile(context.Background(), &domain.ResumeProfile{}, "role")
				return err
			}
			require.Error(t, call())
			require.Error(t, call())
			require.NoError(t, call())
			require.NoError(t, call())
			require.Equal(t, int32(3), calls.Load())
		})
	}
}
