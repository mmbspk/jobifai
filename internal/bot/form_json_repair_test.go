package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
)

func formRepairFixture(t *testing.T) (*domain.ResumeProfile, StepSnapshot, string) {
	t.Helper()
	profile := groundedFixtureProfile()
	snap := makeSnap(SnapField{Ref: "f0", Label: "Describe your relevant experience for this role", Type: SnapTypeTextarea, Required: true, JobContext: "vacancy-one"})
	snap.EmployerKey = "issuer-one"
	proposal, err := json.Marshal([]LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: profile.Summary, Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}}}})
	require.NoError(t, err)
	return profile, snap, string(proposal)
}

func malformedArrayProposal(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal([]map[string]any{{"ref": "f0", "action": "fill", "intent_type": "llm", "binding_key": "", "value": []string{value}}})
	require.NoError(t, err)
	return string(data)
}

func TestFormJSONRepair_ProposalStillNeedsIndependentApprovalAndVerifiedFill(t *testing.T) {
	profile, snap, proposal := formRepairFixture(t)
	proposalRequests := 0
	server := newCountingServer(t, func(body []byte) string {
		if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
			return fixtureGroundedVerdicts(body, true)
		}
		proposalRequests++
		if proposalRequests == 1 {
			return malformedArrayProposal(t, "private-malformed-response")
		}
		require.Contains(t, string(body), "FORM_JSON_FORMAT_REPAIR_PROPOSAL_V1")
		require.NotContains(t, string(body), "private-malformed-response", "repair must not echo failed output")
		return proposal
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "__default__"}
	ledger := &usage.Ledger{DB: store.DB, Catalog: pricing.DefaultCatalog()}
	client := llm.New(domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6", UseProxy: true, ProxyURL: server.server.URL}, "mock-not-a-real-key").WithUserID("__default__").WithBilling(llm.BillingHooks{Ledger: ledger})
	budget := &adaptiveRunBudget{maxCalls: 3}
	ctx := context.WithValue(t.Context(), adaptiveBudgetKey{}, budget)
	result, err := (&StepInterpreter{LLM: boundFormLLM(NewLLMFormInterpreter(client)), Mapping: store}).InterpretStep(ctx, snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	require.Equal(t, ProvenanceVerifiedByModel, result.Fields[0].Provenance)
	require.Equal(t, int32(3), server.requests.Load())
	require.Equal(t, 3, budget.calls, "proposal, repair and verification each consume a request reservation")
	_, cached := store.LookupField(snap.Fields[0].FieldSignature(snap.Platform), snap.EmployerKey)
	require.False(t, cached, "formatting or semantic approval alone does not publish an answer mapping")
	var events, operations int
	require.NoError(t, store.DB.QueryRow("SELECT count(*),count(DISTINCT idempotency_key) FROM llm_usage_events WHERE user_id='__default__'").Scan(&events, &operations))
	require.Equal(t, 3, events, "all three actual provider requests retain billing evidence")
	require.Equal(t, 3, operations)
	writeVerifiedFilledStep(ctx, store, snap, result, StepExecResult{FilledRefs: []string{"f0"}})
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(t.Context(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, int32(3), server.requests.Load())
}

func TestFormJSONRepair_VerifierRepairsFormatOnceWithoutChangingNegativeSemantics(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(fmt.Sprint(approved), func(t *testing.T) {
			profile, snap, proposal := formRepairFixture(t)
			verificationRequests := 0
			server := newCountingServer(t, func(body []byte) string {
				if !strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					return proposal
				}
				verificationRequests++
				if verificationRequests == 1 {
					return "private-invalid-json"
				}
				require.Contains(t, string(body), "FORM_JSON_FORMAT_REPAIR_VERIFICATION_V1")
				require.NotContains(t, string(body), "private-invalid-json")
				return fixtureGroundedVerdicts(body, approved)
			})
			proposals, err := NewLLMFormInterpreter(server.newClient(t)).InterpretFields(t.Context(), profile, snap, []string{"f0"})
			require.NoError(t, err)
			resolution := validateLLMProposals(proposals, []string{"f0"}, snap, profile)
			require.Equal(t, !approved, resolution[0].Pending)
			require.Equal(t, int32(3), server.requests.Load(), "a repaired negative verdict never triggers a fourth request")
		})
	}
}

func TestFormJSONRepair_ValidNegativeMissingFactAndInvalidChoiceDoNotRetry(t *testing.T) {
	profile, snap, proposal := formRepairFixture(t)
	for _, kind := range []string{"negative", "missing fact", "invalid choice"} {
		t.Run(kind, func(t *testing.T) {
			currentSnap := snap
			response := proposal
			if kind == "missing fact" {
				data, err := json.Marshal([]LLMFieldProposal{{Ref: "f0", Action: "skip", IntentType: FieldIntentLLM}})
				require.NoError(t, err)
				response = string(data)
			}
			if kind == "invalid choice" {
				currentSnap = makeSnap(SnapField{Ref: "f0", Label: "When can you start?", Type: SnapTypeRadio, Required: true, Options: []string{"Immediately Available", "Next month"}})
				data, err := json.Marshal([]LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: "Unlisted choice", Evidence: []ProfileEvidence{{Path: "/application_defaults/notice_period", Quote: profile.ApplicationDefaults.NoticePeriod}}}})
				require.NoError(t, err)
				response = string(data)
			}
			server := newCountingServer(t, func(body []byte) string {
				require.NotContains(t, string(body), "FORM_JSON_FORMAT_REPAIR_")
				if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					return fixtureGroundedVerdicts(body, false)
				}
				return response
			})
			proposals, err := NewLLMFormInterpreter(server.newClient(t)).InterpretFields(t.Context(), profile, currentSnap, []string{"f0"})
			require.NoError(t, err)
			require.True(t, validateLLMProposals(proposals, []string{"f0"}, currentSnap, profile)[0].Pending)
			requests := int32(1)
			if kind == "negative" {
				requests = 2
			}
			require.Equal(t, requests, server.requests.Load())
		})
	}
}

func TestFormJSONRepair_ExhaustedAttemptBudgetPreventsExtraHTTP(t *testing.T) {
	for _, phase := range []formJSONPhase{formJSONProposal, formJSONVerification} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			profile, snap, proposal := formRepairFixture(t)
			server := newCountingServer(t, func(body []byte) string {
				if phase == formJSONProposal || strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					return "invalid-json"
				}
				return proposal
			})
			maximum := 1
			if phase == formJSONVerification {
				maximum = 2
			}
			ctx := context.WithValue(t.Context(), adaptiveBudgetKey{}, &adaptiveRunBudget{maxCalls: maximum})
			result, err := (&StepInterpreter{LLM: boundFormLLM(NewLLMFormInterpreter(server.newClient(t)))}).InterpretStep(ctx, snap, profile)
			require.ErrorIs(t, err, ErrLLMLimitExceeded)
			require.False(t, result.Paused, "request exhaustion stays operational")
			require.Equal(t, int32(maximum), server.requests.Load())
		})
	}
}

type formJSONStubClient struct {
	calls int
	run   func(context.Context, []llm.Message, func(string) (string, error)) (string, error)
}

func (c *formJSONStubClient) ChatValidated(ctx context.Context, messages []llm.Message, validate func(string) (string, error)) (string, error) {
	c.calls++
	return c.run(ctx, messages, validate)
}

func formJSONTestValidator(raw string) (string, error) {
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", err
	}
	return raw, nil
}

func TestFormJSONRepair_NonValidatorFailuresNeverRetry(t *testing.T) {
	var malformed any
	jsonErr := json.Unmarshal([]byte("malformed provider envelope"), &malformed)
	for _, cause := range []error{jsonErr, llm.ErrBillingPersistFailed, quota.ErrExceeded, context.DeadlineExceeded, errors.New("private-provider-detail")} {
		client := &formJSONStubClient{run: func(context.Context, []llm.Message, func(string) (string, error)) (string, error) { return "", cause }}
		_, err := chatFormJSON(t.Context(), client, "original prompt", formJSONProposal, formJSONTestValidator)
		require.ErrorIs(t, err, cause)
		require.Equal(t, 1, client.calls)
	}
	client := &formJSONStubClient{run: func(_ context.Context, _ []llm.Message, validate func(string) (string, error)) (string, error) {
		_, validationErr := validate("malformed")
		return "", errors.Join(llm.ErrBillingPersistFailed, validationErr)
	}}
	_, err := chatFormJSON(t.Context(), client, "original prompt", formJSONVerification, formJSONTestValidator)
	require.ErrorIs(t, err, llm.ErrBillingPersistFailed)
	require.Equal(t, 1, client.calls, "a billing error cannot be mistaken for the validator's own failure")
}

func TestFormJSONRepair_CancellationAndAttributionStayBound(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := &formJSONStubClient{run: func(_ context.Context, _ []llm.Message, validate func(string) (string, error)) (string, error) {
		cancel()
		return validate("malformed")
	}}
	_, err := chatFormJSON(ctx, client, "original prompt", formJSONProposal, formJSONTestValidator)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, client.calls)
	for _, cause := range []error{quota.ErrExceeded, llm.ErrBillingPersistFailed} {
		causeCtx, cancelCause := context.WithCancelCause(t.Context())
		client := &formJSONStubClient{run: func(_ context.Context, _ []llm.Message, validate func(string) (string, error)) (string, error) {
			canonical, parseErr := validate("malformed")
			cancelCause(cause)
			return canonical, parseErr
		}}
		_, err := chatFormJSON(causeCtx, client, "original prompt", formJSONProposal, formJSONTestValidator)
		require.ErrorIs(t, err, cause)
		require.Equal(t, 1, client.calls, "typed cancellation causes never permit formatting repair")
	}

	deadlineCtx, done := context.WithTimeout(t.Context(), time.Second)
	defer done()
	deadline, _ := deadlineCtx.Deadline()
	ctx = llm.WithCallContext(deadlineCtx, domain.LLMCallContext{UserID: "owner", JobID: "job", AutomationRunID: "run", OperationID: "parent-operation", CorrelationID: "trace", Attempt: 7})
	client = &formJSONStubClient{}
	client.run = func(callCtx context.Context, messages []llm.Message, validate func(string) (string, error)) (string, error) {
		current, exists := callCtx.Deadline()
		require.True(t, exists)
		require.Equal(t, deadline, current)
		call := llm.CallContextFrom(callCtx)
		require.Equal(t, "owner", call.UserID)
		require.Equal(t, "job", call.JobID)
		require.Equal(t, "run", call.AutomationRunID)
		require.Equal(t, "trace", call.CorrelationID)
		require.Equal(t, domain.TaskFormAnswer, call.Task)
		if client.calls == 1 {
			require.Equal(t, "parent-operation", call.OperationID)
			return validate("malformed")
		}
		require.NotEmpty(t, call.OperationID)
		require.NotEqual(t, "parent-operation", call.OperationID)
		require.Zero(t, call.Attempt)
		require.Contains(t, messages[0].Content, "original prompt")
		return validate("[]")
	}
	_, err = chatFormJSON(ctx, client, "original prompt", formJSONProposal, formJSONTestValidator)
	require.NoError(t, err)
	require.Equal(t, 2, client.calls)
}
