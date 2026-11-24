package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
	"github.com/user/jobifai/internal/resume"
)

// These paired worker replays measure controlled request counts and browser time.
// Mock usage declarations are reported explicitly; they are not production tokens
// or a billing forecast. Every denominator is an observed fixture confirmation.
func TestPairedReplay_LegacyColdWarmAndPartiallyChangedForm(t *testing.T) {
	db := openMappingDB(t)
	settings := config.NewStore(db)
	mappings := &DBMappingStore{DB: db, UserID: "paired"}
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES('paired','paired@fixture.test','fixture')`)
	require.NoError(t, err)
	generationCache := &llmreuse.Store{DB: db}
	server := newGroundedCountingServer(t, func(body []byte) string {
		if strings.Contains(string(body), "Fields to interpret") {
			var proposals []LLMFieldProposal
			for _, ref := range extractRefsFromPromptBody(body) {
				binding := "application_defaults.notice_period"
				if ref == "f2" {
					binding = "application_defaults.form_answers.earliest_start"
				}
				if ref == "f3" {
					binding = "application_defaults.form_answers.current_title"
				}
				proposals = append(proposals, LLMFieldProposal{Ref: ref, Action: "fill", IntentType: FieldIntentProfile, BindingKey: binding})
			}
			encoded, _ := json.Marshal(proposals)
			return string(encoded)
		}
		if strings.Contains(strings.ToLower(string(body)), "earliest available start") {
			if strings.Contains(string(body), "next week") {
				return "next week"
			}
			return "immediately"
		}
		if strings.Contains(strings.ToLower(string(body)), "current or most recent job title") {
			return "Registered Nurse"
		}
		if strings.Contains(string(body), "1 month") {
			return "1 month"
		}
		return "2 weeks"
	})
	client := server.newClient(t).WithUserID("paired").WithReuse(&llm.ReuseCoordinator{Store: generationCache})
	b := New(Config{DB: db, UserID: "paired", Platform: domain.PlatformLinkedIn, Profile: basicProfile(), EngineStore: settings, AdaptiveMapping: mappings, AdaptiveLLM: NewLLMFormInterpreter(client), Tailor: resume.NewTailor(client, client, client, client)})
	br := workerFixtureBrowser(t, func(raw string) string {
		id := linkedInExpectedID(raw)
		html := linkedInWorkerHTML(id, "Acme Inc.")
		fields := `<label for="start">Earliest available start date</label><input id="start">`
		if id == "52345681" || id == "52345682" {
			fields += `<label for="title">Current or most recent job title</label><input id="title">`
		}
		html = strings.Replace(html, `</div><button id="next"`, fields+`</div><button id="next"`, 1)
		expectedStart := "immediately"
		expectedNotice := "2 weeks"
		if id == "52345683" || id == "52345684" {
			expectedStart, expectedNotice = "next week", "1 month"
		}
		html = strings.Replace(html, `localStorage.setItem('lastName'`, `const notice=document.getElementById('notice');if(notice.value!=='`+expectedNotice+`')return;const start=document.getElementById('start');if(start&&start.value!=='`+expectedStart+`')return;const title=document.getElementById('title');if(title&&title.value!=='Registered Nurse')return;localStorage.setItem('lastName'`, 1)
		return html
	})
	for _, scenario := range []struct {
		name, id string
		mode     domain.ApplyEngineMode
		requests int
	}{
		{"legacy_base", "52345678", domain.ApplyEngineLegacy, 2},
		{"adaptive_cold", "52345679", domain.ApplyEngineAdaptive, 2},
		{"adaptive_warm", "52345680", domain.ApplyEngineAdaptive, 0},
		{"adaptive_partial", "52345681", domain.ApplyEngineAdaptive, 2},
		{"legacy_partial", "52345682", domain.ApplyEngineLegacy, 1},
		{"adaptive_profile_changed", "52345683", domain.ApplyEngineAdaptive, 0},
		{"legacy_profile_changed", "52345684", domain.ApplyEngineLegacy, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.name == "adaptive_profile_changed" {
				profile := basicProfile()
				profile.ApplicationDefaults.NoticePeriod = "1 month"
				profile.ApplicationDefaults.FormAnswers["earliest_start"] = "next week"
				b.cfg.Profile = profile
			}
			require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: scenario.mode}))
			before, bytesBefore := server.requests.Load(), server.inputBytes.Load()
			cacheHitsBefore := generationCache.LLMCacheHitCount()
			job := linkedInJob{ID: scenario.id, URL: "https://www.linkedin.com/jobs/view/" + scenario.id, Title: "Coordinator", Company: "Acme Inc."}
			lazy := &lazyDocGen{b: b, ctx: context.Background(), job: job}
			start := time.Now()
			require.True(t, b.submitEasyApply(context.Background(), br, job, lazy, 8, "paired fixture", nil, llm.UsageSnapshot{}))
			duration := time.Since(start).Milliseconds()
			requests := int(server.requests.Load() - before)
			assert.Equal(t, scenario.requests, requests)
			report, _ := json.Marshal(map[string]any{"fixture": "native-linkedin-screening-v1", "scenario": scenario.name, "confirmed_fixture_applications": 1, "provider_requests": requests, "request_bytes": server.inputBytes.Load() - bytesBefore, "declared_mock_input_tokens": 50 * requests, "declared_mock_output_tokens": 25 * requests, "browser_duration_ms": duration, "production_cost_usd": nil, "billing_source": "mock_unpriced", "generation_cache_hits": generationCache.LLMCacheHitCount() - cacheHitsBefore})
			t.Logf("PAIRED_REPLAY %s", report)
			if scenario.name == "adaptive_cold" {
				reviews, err := ListFormMappingReviews(context.Background(), db, b.cfg.UserID)
				require.NoError(t, err)
				require.Len(t, reviews, 2)
				for _, review := range reviews {
					require.Equal(t, string(ProvenanceVerifiedByModel), review.Provenance, "successful verified answers activate cache automatically")
				}
			}
		})
	}
}
