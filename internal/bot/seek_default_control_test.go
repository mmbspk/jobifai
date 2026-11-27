package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A vacancy-specific upload must not ask the candidate to choose a new account
// default. Native and custom versions of that same control have equal meaning.
func TestSeekDocumentDefaultCheckboxIsNotAnApplicationAnswer(t *testing.T) {
	for i, control := range []string{
		`<input id=default-resume type=checkbox><label for=default-resume>Make this my default resumé</label>`,
		`<label for=default-resume></label><label><input id=default-resume type=checkbox>Make this my default résumé</label>`,
		`<label for=default-resume>   </label><label><input id=default-resume type=checkbox>Make this my default résumé</label>`,
		`<div id=default-resume role=checkbox aria-checked=false tabindex=0>Make this my default resumé</div>`,
		`<div id=default-resume role=checkbox aria-checked=false tabindex=0 aria-label="Make this my default résumé"><span aria-hidden=true>Default</span></div>`,
	} {
		t.Run(fmt.Sprintf("variant-%d", i), func(t *testing.T) {
			page := rodTestPage(t, fmt.Sprintf(`<html><body><main data-testid=quick-apply-form><h1>Choose documents</h1><div data-testid=resumeFileInput><input id=resume-upload type=file accept=.pdf></div>%s<button data-testid=continue-button>Continue</button></main></body></html>`, control))
			fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredFields": true, "includeAnsweredRadios": true})
			require.Len(t, fields, 1, "the account default is not a candidate fact")
			require.Equal(t, "file", fields[0].Type)
			snapshot := captureStepSnapshot(fields, nil, "seek", 0, "Choose documents")
			interpretation, err := (&StepInterpreter{}).InterpretStep(context.Background(), snapshot, nil)
			require.NoError(t, err)
			require.False(t, interpretation.Paused)
			require.Len(t, interpretation.Fields, 1)
			require.Equal(t, ActionUpload, interpretation.Fields[0].Action)
			value, err := page.Eval(`()=>document.getElementById('default-resume').getAttribute('aria-checked')==='true'||document.getElementById('default-resume').checked===true`)
			require.NoError(t, err)
			require.False(t, value.Value.Bool())
		})
	}
}

func TestSeekDefaultLikeScreeningCheckboxRemainsAnApplicationAnswer(t *testing.T) {
	page := rodTestPage(t, `<html><body><main data-testid=quick-apply-form><h1>Your application</h1><div id=screening role=checkbox aria-checked=false tabindex=0>I agree to make this résumé available to the recruiter</div><button data-testid=continue-button>Continue</button></main></body></html>`)
	fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredFields": true})
	require.Len(t, fields, 1)
	require.Equal(t, "checkbox", fields[0].Type)
	require.Contains(t, fields[0].Question, "agree")
	interpretation, err := (&StepInterpreter{}).InterpretStep(context.Background(), captureStepSnapshot(fields, nil, "seek", 0, "Your application"), nil)
	require.NoError(t, err)
	require.True(t, interpretation.Paused, "a document preference exception must not grant consent")
}
