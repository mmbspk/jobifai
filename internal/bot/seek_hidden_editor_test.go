package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

type seekInactiveEditorLLM struct{ calls int }

func (m *seekInactiveEditorLLM) InterpretFields(_ context.Context, _ *domain.ResumeProfile, _ StepSnapshot, refs []string) ([]LLMFieldProposal, error) {
	m.calls++
	return []LLMFieldProposal{{Ref: refs[0], Action: string(ActionUpload)}}, nil
}

// The captured SEEK document page retains its inactive "Write a cover letter"
// textarea behind a visible document fieldset. Only the chosen upload controls
// belong in the current step; an inactive editor cannot become an answer hold.
func TestSeekInactiveCoverEditorDoesNotBlockDocumentUploads(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><main data-testid=quick-apply-form><h1>Choose documents</h1><fieldset><legend>Resumé</legend><div data-testid=resumeFileInput><input id=resume-fileFile type=file accept=.pdf style="display:none"><button>Upload</button></div></fieldset><fieldset><legend>Cover letter</legend><label><input type=radio name=coverLetter-method-fixture checked>Upload a cover letter</label><div data-testid=coverLetterFileInput><input id=coverLetter-fileFile type=file accept=.pdf style="display:none"><button>Upload</button></div><div style="display:none"><textarea id=cover-editor data-testid=coverLetterTextInput aria-label="Write a cover letter"></textarea></div></fieldset><button data-testid=continue-button>Continue</button></main></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	fields, _, err := New(Config{Platform: domain.PlatformSeek}).scanAdaptiveStep(t.Context(), page)
	require.NoError(t, err)
	llm := &seekInactiveEditorLLM{}
	snapshot := captureStepSnapshot(fields, nil, "seek", 0, "Choose documents")
	interpretation, err := (&StepInterpreter{LLM: llm}).InterpretStep(t.Context(), snapshot, nil)
	require.NoError(t, err)
	require.False(t, interpretation.Paused, "inactive document UI must not defer an application for a candidate answer")
	require.Zero(t, llm.calls, "only native document uploads should be interpreted on this step")
	require.Len(t, fields, 2)
	for _, field := range interpretation.Fields {
		require.Equal(t, ActionUpload, field.Action)
	}
}

func TestSeekRenderedTextareaBelowFoldRemainsInSnapshot(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><main data-testid=quick-apply-form><h1>Employer questions</h1><div style="height:1600px"></div><fieldset><legend>Supporting statement</legend><textarea id=statement aria-label="Supporting statement" style="width:300px;height:100px"></textarea></fieldset><button data-testid=continue-button>Continue</button></main></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	fields, _, err := New(Config{Platform: domain.PlatformSeek}).scanAdaptiveStep(t.Context(), page)
	require.NoError(t, err)
	require.Len(t, fields, 1, "rendered vertical offscreen questions still belong in the current form")
	require.Equal(t, "Supporting statement", fields[0].Question)
	require.Equal(t, SnapTypeTextarea, captureStepSnapshot(fields, nil, "seek", 0, "Employer questions").Fields[0].Type)
}
