package bot

// adaptive_llm_provider_test.go — provider-facing tests for LLMFormInterpreter (Finding 4).
//
// These tests use a counting mock HTTP server and the production LLMFormInterpreter
// (not stubLLM) to prove the batch adapter's cost properties end-to-end:
//
//   Cold step     : 3 unresolved fields → proposal + semantic verification batches.
//   Warm step     : all 3 fields in mapping cache → 0 provider requests.
//   Partial miss  : cached fields stay local; only new refs reach both batches.
//
// Evidence is published via t.Logf and can be verified against the mapping store.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
)

// ── mock HTTP server ──────────────────────────────────────────────────────────

// countingClaudeServer returns a mock server that counts requests and responds
// with a Claude-shaped JSON envelope whose text field is the last JSON array
// returned by responseFor (called with the request body).
//
// responseFor receives the raw request body and must return a valid JSON array
// string of LLMFieldProposal objects. If nil, every call returns an empty array.
type countingServer struct {
	server      *httptest.Server
	requests    atomic.Int32
	inputBytes  atomic.Int64
	responseFor func(body []byte) string // may be nil → returns "[]"
}

func newCountingServer(t *testing.T, responseFor func(body []byte) string) *countingServer {
	t.Helper()
	cs := &countingServer{responseFor: responseFor}
	cs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.requests.Add(1)
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		}
		text := "[]"
		cs.inputBytes.Add(int64(len(body)))
		if cs.responseFor != nil {
			text = cs.responseFor(body)
		}
		resp := map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"usage":   map[string]any{"input_tokens": 50, "output_tokens": 25},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(cs.server.Close)
	return cs
}

func (cs *countingServer) newClient(t *testing.T) *llm.Client {
	t.Helper()
	cfg := domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-mock",
		UseProxy: true,
		ProxyURL: cs.server.URL,
	}
	return llm.New(cfg, "mock-api-key-not-real")
}

// ── helpers ──────────────────────────────────────────────────────────────────

// providerThreeFieldSnap returns the same 3-field snapshot used in mapping_cache_proof_test.go
// so the mapping DB entries are compatible.
func providerThreeFieldSnap() StepSnapshot {
	return captureStepSnapshot([]formField{
		{Type: "text", Question: "Total years of work experience", InputType: "text"},
		{Type: "radio", Question: "Do you have a driver's licence?", InputType: "radio",
			Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}},
		{Type: "text", Question: "Current or most recent job title", InputType: "text"},
	}, nil, "linkedin", 0, "Screening Questions")
}

// coldProposalsJSON returns a JSON array matching providerThreeFieldSnap fields.
func coldProposalsJSON() string {
	proposals := []map[string]any{
		{"ref": "f0", "action": "fill", "intent_type": "profile", "binding_key": "application_defaults.form_answers.experience_years", "value": ""},
		{"ref": "f1", "action": "fill", "intent_type": "profile", "binding_key": "application_defaults.form_answers.drivers_licence", "value": ""},
		{"ref": "f2", "action": "fill", "intent_type": "profile", "binding_key": "application_defaults.form_answers.current_title", "value": ""},
	}
	b, _ := json.Marshal(proposals)
	return string(b)
}

// partialProposalJSON returns a JSON array for only the new field (f3) in a 4-field partial snap.
func partialProposalJSON(ref string) string {
	proposals := []map[string]any{
		{"ref": ref, "action": "fill", "intent_type": "profile", "binding_key": "application_defaults.form_answers.earliest_start", "value": ""},
	}
	b, _ := json.Marshal(proposals)
	return string(b)
}

// openProviderTestDB opens a fresh SQLite mapping DB for a test.
func openProviderTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "provider_test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestLLMFormInterpreter_ColdStep_TwoBatchedProviderRequests proves that a cold step with
// 3 unresolved fields issues two batched requests containing all 3 refs.
func TestLLMFormInterpreter_ColdStep_TwoBatchedProviderRequests(t *testing.T) {
	var capturedRefs []string
	srv := newGroundedCountingServer(t, func(body []byte) string {
		// Extract the refs from the prompt body to verify all 3 are sent.
		capturedRefs = extractRefsFromPromptBody(body)
		return coldProposalsJSON()
	})

	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-provider-cold"}
	interp := &LLMFormInterpreter{client: srv.newClient(t)}
	si := &StepInterpreter{LLM: interp, Mapping: store}

	snap := providerThreeFieldSnap()
	result, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)

	// Write verified resolutions to the cache.
	require.NoError(t, store.Write(context.Background(), snap, result.Fields))

	requests := int(srv.requests.Load())
	t.Logf("EVIDENCE provider-cold | provider_requests=%d | refs_sent=%v | llm_used=%v | paused=%v",
		requests, capturedRefs, result.LLMUsed, result.Paused)

	assert.Equal(t, 2, requests, "cold step: proposal and independent verification HTTP requests")
	assert.Equal(t, 3, len(result.Fields), "cold step: 3 fields resolved")
	assert.True(t, result.LLMUsed, "cold step: LLMUsed must be true")
	assert.False(t, result.Paused, "cold step: no paused fields")

	// Verify the values the adapter returned.
	vals := make(map[string]string)
	for _, r := range result.Fields {
		vals[r.Ref] = r.Value
	}
	assert.Equal(t, "7", vals["f0"])
	assert.Equal(t, "No", vals["f1"])
	assert.Equal(t, "Registered Nurse", vals["f2"])

	// Verify label cost estimate.
	estimateLabelCost(t, 3)
}

func TestLLMFormInterpreter_ConcurrentMissesCoalesceWithinVerifiedScope(t *testing.T) {
	db := openProviderTestDB(t)
	for _, id := range []string{"owner", "other"} {
		_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(?,?,?)`, id, id+"@fixture.test", "fixture")
		require.NoError(t, err)
	}
	server := newGroundedCountingServer(t, func([]byte) string {
		time.Sleep(100 * time.Millisecond)
		return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"application_defaults.notice_period"}]`
	})
	client := server.newClient(t).WithUserID("owner").WithReuse(&llm.ReuseCoordinator{Store: &llmreuse.Store{DB: db}})
	adapter := NewLLMFormInterpreter(client)
	snap := makeSnap(textField("f0", "How soon can you leave?"))
	snap.EmployerKey = "verified-issuer-a"
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := adapter.InterpretFields(context.Background(), basicProfile(), snap, []string{"f0"})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(2), server.requests.Load(), "one proposal and one independent verification request for eight concurrent misses")
	changed := snap
	changed.EmployerKey = "verified-issuer-b"
	_, err := adapter.InterpretFields(context.Background(), basicProfile(), changed, []string{"f0"})
	require.NoError(t, err)
	current := basicProfile()
	current.ApplicationDefaults.NoticePeriod = "1 month"
	_, err = adapter.InterpretFields(context.Background(), current, snap, []string{"f0"})
	require.NoError(t, err)
	other := NewLLMFormInterpreter(server.newClient(t).WithUserID("other").WithReuse(&llm.ReuseCoordinator{Store: &llmreuse.Store{DB: db}}))
	_, err = other.InterpretFields(context.Background(), basicProfile(), snap, []string{"f0"})
	require.NoError(t, err)
	assert.Equal(t, int32(8), server.requests.Load(), "employer, profile and user changes require scoped proposal and verification batches")
}

// TestLLMFormInterpreter_WarmStep_ZeroProviderRequests proves that when all 3 fields
// are in the mapping cache, the LLMFormInterpreter is never invoked (zero HTTP requests).
func TestLLMFormInterpreter_WarmStep_ZeroProviderRequests(t *testing.T) {
	srv := newGroundedCountingServer(t, func(_ []byte) string {
		return "[]" // must never be called
	})

	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-provider-warm"}

	// Seed the mapping cache directly (simulating a prior cold run).
	snap := providerThreeFieldSnap()
	seedResolutions := []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.experience_years"},
		{Ref: "f1", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.drivers_licence"},
		{Ref: "f2", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"},
	}
	require.NoError(t, reviewFixtureMappings(t, store, snap, seedResolutions))

	// Now run the interpreter with the production adapter (should not be called).
	interpreter := &LLMFormInterpreter{client: srv.newClient(t)}
	si := &StepInterpreter{LLM: interpreter, Mapping: store}

	result, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)

	requests := int(srv.requests.Load())
	fromMapping := 0
	for _, r := range result.Fields {
		if r.Source == SourceMapping {
			fromMapping++
		}
	}

	t.Logf("EVIDENCE provider-warm | provider_requests=%d | fields_from_mapping=%d | llm_used=%v",
		requests, fromMapping, result.LLMUsed)

	assert.Equal(t, 0, requests, "warm step: zero provider HTTP requests")
	assert.Equal(t, 3, fromMapping, "warm step: all 3 fields served from mapping cache")
	assert.False(t, result.LLMUsed, "warm step: LLMUsed must be false")
	assert.False(t, result.Paused, "warm step: no paused fields")
}

// TestLLMFormInterpreter_PartialMiss_TwoBatchesWithNewField proves that when 2 of 3
// fields are in the mapping cache and 1 is new, two batched requests contain only
// the new field's ref.
func TestLLMFormInterpreter_PartialMiss_TwoBatchesWithNewField(t *testing.T) {
	const newFieldRef = "f3"
	var capturedRefs []string
	srv := newGroundedCountingServer(t, func(body []byte) string {
		capturedRefs = extractRefsFromPromptBody(body)
		return partialProposalJSON(newFieldRef)
	})

	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-provider-partial"}

	// Seed 3 known fields.
	snap3 := providerThreeFieldSnap()
	seedResolutions := []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.experience_years"},
		{Ref: "f1", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.drivers_licence"},
		{Ref: "f2", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.current_title"},
	}
	require.NoError(t, reviewFixtureMappings(t, store, snap3, seedResolutions))

	// Partial snap: same 3 known fields + 1 new field.
	snap4 := captureStepSnapshot([]formField{
		{Type: "text", Question: "Total years of work experience", InputType: "text"},
		{Type: "radio", Question: "Do you have a driver's licence?", InputType: "radio",
			Options: []formFieldOption{{Label: "Yes"}, {Label: "No"}}},
		{Type: "text", Question: "Current or most recent job title", InputType: "text"},
		{Type: "text", Question: "Earliest available start date", InputType: "text"},
	}, nil, "linkedin", 0, "Screening Questions")

	interpreter := &LLMFormInterpreter{client: srv.newClient(t)}
	si := &StepInterpreter{LLM: interpreter, Mapping: store}

	result, err := si.InterpretStep(context.Background(), snap4, basicProfile())
	require.NoError(t, err)

	requests := int(srv.requests.Load())
	fromMapping := 0
	for _, r := range result.Fields {
		if r.Source == SourceMapping {
			fromMapping++
		}
	}

	t.Logf("EVIDENCE provider-partial | provider_requests=%d | refs_sent_to_provider=%v | fields_from_mapping=%d | total_fields=%d",
		requests, capturedRefs, fromMapping, len(result.Fields))

	assert.Equal(t, 2, requests, "partial miss: one proposal and one verification batch for the new field")
	assert.Equal(t, []string{newFieldRef}, capturedRefs, "partial miss: only the new field ref sent to provider")
	assert.Equal(t, 3, fromMapping, "partial miss: 3 known fields served from mapping cache")
	assert.Equal(t, 4, len(result.Fields), "partial miss: all 4 fields resolved")

	// New field value from provider.
	vals := make(map[string]string)
	for _, r := range result.Fields {
		vals[r.Ref] = r.Value
	}
	assert.Equal(t, "immediately", vals[newFieldRef], "partial miss: new field value from provider")
}

// ── helpers ──────────────────────────────────────────────────────────────────

// extractRefsFromPromptBody parses the Claude request body and extracts the
// field refs mentioned in the prompt. The request body is a JSON object with
// a "messages" array; we scan the concatenated text for "\"ref\":\"fN\"" patterns.
func extractRefsFromPromptBody(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	// The LLM request body is {"model":"...","messages":[{"role":"user","content":"..."}],...}
	// The content is a string containing the prompt. We unmarshal to extract it.
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	var refs []string
	seen := make(map[string]bool)
	for _, msg := range req.Messages {
		// Extract from the JSON fields array embedded in the prompt.
		// Look for "\"ref\":\"fN\"" patterns.
		content := msg.Content
		for i := 0; i < len(content)-6; i++ {
			if content[i:i+6] == `"ref":` {
				// Find the value after "ref":"
				start := i + 6
				for start < len(content) && (content[start] == '"' || content[start] == ' ') {
					if content[start] == '"' {
						start++
						break
					}
					start++
				}
				end := start
				for end < len(content) && content[end] != '"' {
					end++
				}
				if end > start {
					ref := content[start:end]
					if !seen[ref] && len(ref) > 0 && ref[0] == 'f' {
						seen[ref] = true
						refs = append(refs, ref)
					}
				}
			}
		}
	}
	return refs
}

// estimateLabelCost reports the mock's declared usage without invented rates.
func estimateLabelCost(t *testing.T, fieldCount int) {
	t.Helper()
	t.Logf("FIXTURE USAGE | fields=%d | declared_mock_input_tokens=50 | declared_mock_output_tokens=25 | production_cost=unknown", fieldCount)
}
