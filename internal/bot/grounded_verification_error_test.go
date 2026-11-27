package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
)

func TestGroundedVerification_MalformedResponseKeepsCauseWithoutApproval(t *testing.T) {
	for _, test := range []struct {
		name, response string
		syntax         bool
	}{
		{"syntax", "private-candidate-response", true},
		{"boolean type", `[{"ref":"f0","supported":"private-candidate-response","answers_question":true,"missing_fact":false}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := groundedFixtureProfile()
			snap := makeSnap(SnapField{Ref: "f0", Label: "Describe your relevant experience for this role", Type: SnapTypeTextarea, Required: true})
			proposal, err := json.Marshal([]LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentLLM, Value: profile.Summary, Evidence: []ProfileEvidence{{Path: "/summary", Quote: profile.Summary}}}})
			require.NoError(t, err)
			server := newCountingServer(t, func(body []byte) string {
				if strings.Contains(string(body), "VERIFY_GROUNDED_FORM_ANSWERS_V1") {
					return test.response
				}
				return string(proposal)
			})
			proposals, err := NewLLMFormInterpreter(server.newClient(t)).InterpretFields(t.Context(), profile, snap, []string{"f0"})
			require.Error(t, err)
			require.Empty(t, proposals, "malformed verification cannot create a grounded answer")
			if test.syntax {
				var syntax *json.SyntaxError
				require.ErrorAs(t, err, &syntax)
			} else {
				var valueType *json.UnmarshalTypeError
				require.ErrorAs(t, err, &valueType)
			}
			require.False(t, errors.Is(err, ErrLLMUnavailable))
			require.Equal(t, PendingMalformedOutput, pendingReasonFromErr(err))
			require.NotContains(t, err.Error(), "private-candidate-response")
			require.Equal(t, int32(3), server.requests.Load(), "one explicitly reserved formatting repair remains bounded and cannot approve malformed output")
		})
	}
}

func TestGroundedVerification_OperationalCausesRemainTypedAndPrivate(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, llm.ErrBillingPersistFailed, quota.ErrExceeded} {
		err := &groundedVerificationError{cause: fmt.Errorf("private-provider-detail: %w", cause)}
		require.ErrorIs(t, err, cause)
		require.NotContains(t, err.Error(), "private-provider-detail")
		require.False(t, errors.Is(err, ErrLLMUnavailable))
		if errors.Is(cause, quota.ErrExceeded) {
			require.ErrorIs(t, err, ErrLLMLimitExceeded)
			require.Equal(t, PendingLimitExceeded, pendingReasonFromErr(err))
		}
	}
	err := &groundedVerificationError{cause: errors.New("private-provider-detail")}
	require.ErrorIs(t, err, ErrLLMUnavailable)
	require.NotContains(t, err.Error(), "private-provider-detail")
}
