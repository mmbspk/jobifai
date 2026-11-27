package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

func TestCheckboxValues_ExactCSVChoicesPreserveCardinalityAndAmbiguityGuards(t *testing.T) {
	field := SnapField{Type: SnapTypeCheckbox, Options: []string{"jQuery", "React.js", "Node.js", "None of these"}}
	for _, tc := range []struct {
		value string
		want  []string
		ok    bool
	}{
		{"React.js", []string{"React.js"}, true},
		{"React.js, Node.js", []string{"React.js", "Node.js"}, true},
		{"node.js, react.js", []string{"React.js", "Node.js"}, true},
		{"React.js, Unknown.js", nil, false},
		{"React.js, react.js", nil, false},
		{"React.js, None of these", nil, false},
		{"React.js,", nil, false},
		{"React.js" + string(rune(10)) + "Node.js", nil, false},
	} {
		values, ok := checkboxSelections(field, tc.value)
		require.Equal(t, tc.ok, ok, tc.value)
		require.Equal(t, tc.want, values, tc.value)
	}
	encoded, err := json.Marshal([]string{"React.js", "Node.js"})
	require.NoError(t, err)
	_, ok := checkboxSelections(field, string(encoded))
	require.False(t, ok, "an array representation is not silently coerced")
	require.False(t, bindingValueCompatible(SnapField{Type: SnapTypeRadio, Options: field.Options}, "", "React.js, Node.js"))
	require.False(t, bindingValueCompatible(SnapField{Type: SnapTypeSelect, Options: field.Options}, "", "React.js, Node.js"))
	duplicate := field
	duplicate.Options = append(duplicate.Options, "react.js")
	_, ok = checkboxSelections(duplicate, "React.js, Node.js")
	require.False(t, ok)
	commas := SnapField{Type: SnapTypeCheckbox, Options: []string{"Paris, France", "Berlin, Germany"}}
	values, ok := checkboxSelections(commas, fmt.Sprintf("%q,%q", commas.Options[0], commas.Options[1]))
	require.True(t, ok)
	require.Equal(t, commas.Options, values)
}

func TestCheckboxValues_NativeFillChecksEveryChoiceAndRerender(t *testing.T) {
	for _, variation := range []string{"normal", "rerender controls", "repurposed label", "disabled selected", "duplicate control"} {
		t.Run(variation, func(t *testing.T) {
			body := checkboxCaptionCard("frameworks", "Which frameworks are you proficient in?", "jQuery", "React.js", "Node.js")
			script := ""
			switch variation {
			case "rerender controls":
				script = "document.getElementById('frameworks-1').addEventListener('change',()=>{const old=document.getElementById('frameworks-2');old.replaceWith(old.cloneNode(true));});"
			case "repurposed label":
				script = "document.getElementById('frameworks-1').addEventListener('change',()=>{document.querySelector('label[for=frameworks-2]').textContent='None of these';});"
			case "disabled selected":
				script = "document.getElementById('frameworks-2').disabled=true;"
			case "duplicate control":
				script = "document.querySelector('form').append(document.getElementById('frameworks-2').cloneNode(true));"
			}
			page := rodTestPage(t, "<html><body><form data-testid='quick-apply-form'>"+body+"<label><input id='default-resume' type='checkbox' checked>Make this my default résumé</label></form><script>"+script+"</script></body></html>")
			fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredFields": true})
			snap := captureStepSnapshot(fields, nil, "seek", 0, "")
			require.NotEmpty(t, snap.Fields)
			result := fillAdaptiveControl(t.Context(), page, snap.Fields[0], "React.js, Node.js")
			success := variation == "normal" || variation == "rerender controls"
			require.Equal(t, success, result.Filled)
			state, err := page.Eval("()=>({jQuery:document.getElementById('frameworks-0').checked,react:document.getElementById('frameworks-1').checked,node:document.getElementById('frameworks-2').checked,defaultResume:document.getElementById('default-resume').checked})")
			require.NoError(t, err)
			require.False(t, state.Value.Get("jQuery").Bool())
			require.True(t, state.Value.Get("defaultResume").Bool())
			if success {
				require.True(t, state.Value.Get("react").Bool())
				require.True(t, state.Value.Get("node").Bool())
			}
		})
	}
}

func TestCheckboxValues_GroundedAllChoicesLearnOnlyAfterVerifiedFill(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(map[bool]string{true: "supported selections", false: "unsupported additional selection"}[approved], func(t *testing.T) {
			profile := basicProfile()
			profile.Summary = "I am proficient in React.js and Node.js."
			field := SnapField{Ref: "f0", Label: "Which frameworks are you proficient in?", Type: SnapTypeCheckbox, Options: []string{"jQuery", "React.js", "Node.js"}, Required: true}
			snap := makeSnap(field)
			snap.EmployerKey = "fixture-issuer"
			value := "React.js, Node.js"
			if !approved {
				value = "jQuery, React.js"
			}
			proposal, err := json.Marshal([]LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: value, Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}}}})
			require.NoError(t, err)
			server := newCountingServer(t, func(body []byte) string {
				if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					missing := !approved
					verdict, marshalErr := json.Marshal([]groundedVerificationVerdict{{Ref: "f0", Supported: approved, AnswersQuestion: true, MissingFact: &missing}})
					require.NoError(t, marshalErr)
					return string(verdict)
				}
				return string(proposal)
			})
			database := openMappingDB(t)
			store := &DBMappingStore{DB: database, UserID: "checkbox-candidate"}
			interpreter := &StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}
			result, err := interpreter.InterpretStep(t.Context(), snap, profile)
			require.NoError(t, err)
			require.Equal(t, int32(2), server.requests.Load(), "every selection still needs the independent verifier")
			require.Equal(t, !approved, result.Paused)
			require.Empty(t, writeVerifiedFilledStep(t.Context(), store, snap, result, StepExecResult{}))
			_, present := store.LookupField(field.FieldSignature(snap.Platform), snap.EmployerKey)
			require.False(t, present, "proposal approval alone cannot publish a learned answer")
			if !approved {
				return
			}
			require.Len(t, result.Fields, 1)
			require.Equal(t, "React.js,Node.js", result.Fields[0].Value)
			require.NotNil(t, result.Fields[0].grounded)
			require.Equal(t, []string{"f0"}, writeVerifiedFilledStep(t.Context(), store, snap, result, StepExecResult{FilledRefs: []string{"f0"}}))
			interpreter.LLM = nil
			warm, err := interpreter.InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.False(t, warm.Paused)
			require.Equal(t, SourceMapping, warm.Fields[0].Source)
			require.Equal(t, "React.js,Node.js", warm.Fields[0].Value)
		})
	}
}

func TestCheckboxValues_SeekWorkerSubmitsSupportedSetAndLearnsIt(t *testing.T) {
	database := openMappingDB(t)
	settings := config.NewStore(database)
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	profile := basicProfile()
	profile.Summary = "I am proficient in React.js and Node.js."
	server := newCountingServer(t, func(body []byte) string {
		if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
			return fixtureGroundedVerdicts(body, true)
		}
		var proposals []LLMFieldProposal
		for _, ref := range extractRefsFromPromptBody(body) {
			proposals = append(proposals, LLMFieldProposal{Ref: ref, Action: "fill", IntentType: FieldIntentLLM, Value: "React.js, Node.js", Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}}})
		}
		raw, err := json.Marshal(proposals)
		require.NoError(t, err)
		return string(raw)
	})
	store := &DBMappingStore{DB: database, UserID: "worker-checkbox"}
	b := New(Config{DB: database, UserID: store.UserID, Platform: domain.PlatformSeek, Profile: profile, EngineStore: settings, AdaptiveMapping: store, AdaptiveLLM: NewLLMFormInterpreter(server.newClient(t))})
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMin = 1
	b.cfg.Settings.HumanBehavior.PauseBetweenJobsMax = 2
	browser := workerFixtureBrowser(t, func(raw string) string {
		page := seekWorkerHTML(raw)
		if !strings.Contains(raw, "/apply") {
			return page
		}
		page = strings.Replace(page, "<label for=\"notice\">How soon can you leave?</label><input id=\"notice\">", checkboxCaptionCard("frameworks", "Which frameworks are you proficient in?", "jQuery", "React.js", "Node.js"), 1)
		page = strings.Replace(page, "localStorage.setItem('lastNotice',document.getElementById('notice').value);", "const selected=[...document.querySelectorAll('input[name=frameworks]:checked')];if(selected.length!==2)return;localStorage.setItem('selected-framework-count',String(selected.length));", 1)
		return strings.Replace(page, "form.innerHTML='<h1 data-automation=\"application-success-title\">Application submitted</h1>'", "history.pushState({},'', '/job/82345681/apply/success');form.innerHTML='"+seekSentBannerFixture+"'", 1)
	})
	job := seekJob{ID: "82345681", URL: "https://au.seek.com/job/82345681", Title: "Coordinator", Company: "Acme Inc.", EasyApply: true}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{ID: job.ID, URL: job.URL, Title: job.Title, Company: job.Company}}
	require.Equal(t, seekSubmitApplied, b.submitSeekApplication(ctx, browser, job, lazy, 8, "fixture", nil, llm.UsageSnapshot{}))
	var confirmed int
	require.NoError(t, database.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", store.UserID, job.ID).Scan(&confirmed))
	require.Equal(t, 1, confirmed)
	var value string
	require.NoError(t, database.QueryRow("SELECT resolved_value FROM form_field_mappings WHERE user_id=? AND field_label='Which frameworks are you proficient in?' AND provenance='verified_by_model'", store.UserID).Scan(&value))
	require.Equal(t, "React.js,Node.js", value)
	probe := navigateAndWait(t, browser, job.URL)
	count, err := probe.Eval("()=>localStorage.getItem('selected-framework-count')")
	require.NoError(t, err)
	require.Equal(t, "2", count.Value.String())
}
