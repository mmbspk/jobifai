package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveControl_NativeTypesRetainDeclaredValues(t *testing.T) {
	cases := []struct{ name, html, answer, probe, expected string }{
		{"text", `<label for="field">Full name</label><input id="field">`, "Jane Smith", `() => document.getElementById('field').value`, "Jane Smith"},
		{"number", `<label for="field">Years of experience</label><input id="field" type="number" required>`, "7", `() => document.getElementById('field').value`, "7"},
		{"textarea", `<label for="field">Additional details</label><textarea id="field"></textarea>`, "Available for an interview", `() => document.getElementById('field').value`, "Available for an interview"},
		{"select", `<label for="field">Country</label><select id="field"><option value="">Choose</option><option value="AU">Australia</option><option value="NZ">New Zealand</option></select>`, "Australia", `() => document.getElementById('field').value`, "AU"},
		{"radio", `<fieldset><legend>Preferred shift</legend><label><input id="morning" name="shift" type="radio" value="morning">Morning</label><label><input id="night" name="shift" type="radio" value="night">Night</label></fieldset>`, "Night", `() => document.getElementById('night').checked ? 'selected' : 'empty'`, "selected"},
		{"checkbox", `<fieldset><legend>Language</legend><label><input id="english" name="language" type="checkbox" value="english">English</label><label><input id="arabic" name="language" type="checkbox" value="arabic">Arabic</label></fieldset>`, "Arabic", `() => document.getElementById('arabic').checked ? 'selected' : 'empty'`, "selected"},
		{"prechecked checkbox group", `<fieldset><legend>Language</legend><label><input id="english" name="language" type="checkbox" value="english" checked>English</label><label><input id="arabic" name="language" type="checkbox" value="arabic">Arabic</label></fieldset>`, "Arabic", `() => document.getElementById('arabic').checked && !document.getElementById('english').checked ? 'selected exclusively' : 'incorrect group'`, "selected exclusively"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form">`+tc.html+`</form></body></html>`)
			b := newTestBot(t)
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "seek", 0, "Application")
			require.Len(t, snap.Fields, 1)
			result := (&seekRodFiller{page: page, bot: b}).Fill(context.Background(), snap.Fields[0], FieldResolution{Ref: "f0", Action: ActionFill, Value: tc.answer})
			require.NoError(t, result.Err)
			require.True(t, result.Filled)
			value, err := page.Eval(tc.probe)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, value.Value.String())
		})
	}
}

func TestAdaptiveControl_TypeaheadRequiresAssociatedAcceptedSuggestion(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		duplicates, ignoreClick, unrelated bool
		success                            bool
	}{
		{name: "accepted", success: true}, {name: "duplicate", duplicates: true}, {name: "ignored click", ignoreClick: true}, {name: "unrelated list", unrelated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := `<li role="option">Sydney</li><li role="option">Melbourne</li>`
			if tc.duplicates {
				options += `<li role="option">Sydney</li>`
			}
			listID := "cities"
			if tc.unrelated {
				listID = "unrelated"
			}
			page := rodTestPage(t, fmt.Sprintf(`<html><body><form data-testid="quick-apply-form"><label for="city">City</label><input id="city" role="combobox" aria-controls="cities" aria-expanded="true"><ul id="%s" role="listbox" hidden>%s</ul></form><script>
			window.clicks=0;window.trusted=false;const input=document.getElementById('city'),list=document.querySelector('ul');
			input.addEventListener('input',e=>{window.trusted=e.isTrusted;if(e.isTrusted)list.hidden=false;});
			for(const option of list.children)option.addEventListener('click',()=>{window.clicks++;if(%t)return;input.value=option.textContent;input.setAttribute('aria-expanded','false');list.hidden=true;});
			</script></body></html>`, listID, options, tc.ignoreClick))
			b := newTestBot(t)
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "seek", 0, "")
			require.Len(t, snap.Fields, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result := fillAdaptiveControl(ctx, page, snap.Fields[0], "Sydney")
			if tc.success {
				require.NoError(t, result.Err)
				require.True(t, result.Filled)
			} else {
				require.Error(t, result.Err)
				require.False(t, result.Filled)
			}
			trusted, err := page.Eval(`()=>window.trusted`)
			require.NoError(t, err)
			assert.True(t, trusted.Value.Bool())
			clicks, err := page.Eval(`()=>window.clicks`)
			require.NoError(t, err)
			if tc.duplicates || tc.unrelated {
				assert.Zero(t, clicks.Value.Int())
			} else {
				assert.Equal(t, 1, clicks.Value.Int())
			}
		})
	}
}

func TestAdaptiveControl_LargeCountryListResolvesLocallyAndTracksAllChoices(t *testing.T) {
	options := ""
	for i := 0; i < 25; i++ {
		options += fmt.Sprintf(`<option value="%d">Country %d</option>`, i, i)
	}
	options += `<option value="AU">Australia</option>`
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="country">Country</label><select id="country">`+options+`</select></form></body></html>`)
	b := newTestBot(t)
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "seek", 0, "")
	require.Len(t, snap.Fields[0].Options, maxSnapOptions)
	llm := &stubLLM{}
	interp, err := (&StepInterpreter{LLM: llm}).InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, interp.Paused)
	assert.Zero(t, llm.calls)
	result := fillAdaptiveControl(context.Background(), page, snap.Fields[0], interp.Fields[0].Value)
	require.NoError(t, result.Err)
	value, err := page.Eval(`()=>document.getElementById('country').value`)
	require.NoError(t, err)
	assert.Equal(t, "AU", value.Value.String())
	_, err = page.Eval(`()=>document.querySelector('option[value=AU]').value='other'`)
	require.NoError(t, err)
	result = fillAdaptiveControl(context.Background(), page, snap.Fields[0], "Australia")
	require.Error(t, result.Err, "changed choice outside the bounded list must invalidate the control")
}

func TestAdaptiveControl_PrepopulatedValuesResolveFromCurrentProfile(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="field">First name</label><input id="field" value="Old name"></form></body></html>`)
	b := newTestBot(t)
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "seek", 0, "Application")
	require.Len(t, snap.Fields, 1)
	interp, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.False(t, interp.Paused)
	result := (&seekRodFiller{page: page, bot: b}).Fill(context.Background(), snap.Fields[0], interp.Fields[0])
	require.NoError(t, result.Err)
	value, err := page.Eval(`() => document.getElementById('field').value`)
	require.NoError(t, err)
	assert.Equal(t, basicProfile().PersonalInformation.Name, value.Value.String())
}

func TestAdaptiveControl_ReusedIDWithChangedQuestionCannotFill(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label id="label" for="field">First name</label><input id="field"></form></body></html>`)
	b := newTestBot(t)
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "seek", 0, "Application")
	require.Len(t, snap.Fields, 1)
	_, err = page.Eval(`() => { document.getElementById('label').textContent = 'Previous employer name'; }`)
	require.NoError(t, err)
	result := (&seekRodFiller{page: page, bot: b}).Fill(context.Background(), snap.Fields[0], FieldResolution{Ref: "f0", Action: ActionFill, Value: "Jane"})
	require.Error(t, result.Err)
	assert.False(t, result.Filled)
	value, err := page.Eval(`() => document.getElementById('field').value`)
	require.NoError(t, err)
	assert.Empty(t, value.Value.String())
}

func TestAdaptiveControl_AmbiguousLabelsPauseBeforeActions(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="one">Years of experience</label><input id="one"><label for="two">Years of experience</label><input id="two"></form></body></html>`)
	res, err := page.Eval(jsScanFields, map[string]any{"includeAnsweredFields": true})
	require.NoError(t, err)
	var fields []formField
	require.NoError(t, json.Unmarshal([]byte(res.Value.String()), &fields))
	snap := captureStepSnapshot(fields, nil, "seek", 0, "Application")
	llm := &stubLLM{}
	interp, err := (&StepInterpreter{LLM: llm}).InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.True(t, interp.Paused)
	assert.Zero(t, llm.calls)
	for _, field := range interp.Fields {
		assert.Equal(t, PendingAmbiguous, field.Reason)
	}
}

func TestAdaptiveControl_ChangedConstraintsRejectStaleFill(t *testing.T) {
	for _, mutation := range []string{
		`document.getElementById('field').min='2'`,
		`document.getElementById('field').max='20'`,
		`document.getElementById('field').step='2'`,
		`document.getElementById('field').pattern='[0-9]+'`,
		`document.documentElement.lang='de'`,
		`document.getElementById('field').type='text'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			page := rodTestPage(t, `<html lang="en"><body><form data-testid="quick-apply-form"><label for="field">Salary</label><input id="field" type="number" min="0" max="10" step="1"></form></body></html>`)
			b := newTestBot(t)
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "seek", 0, "")
			require.Len(t, snap.Fields, 1)
			assert.Equal(t, "number", snap.Fields[0].InputType)
			assert.Equal(t, "10", snap.Fields[0].MaxValue)
			_, err = page.Eval(`()=>{` + mutation + `}`)
			require.NoError(t, err)
			result := fillAdaptiveControl(context.Background(), page, snap.Fields[0], "4")
			require.Error(t, result.Err)
			value, err := page.Eval(`()=>document.getElementById('field').value`)
			require.NoError(t, err)
			assert.Empty(t, value.Value.String(), "no input before the changed control is reinterpreted")
		})
	}
}

func TestAdaptiveControl_JobScopeDoesNotPreventCurrentControlBinding(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="field">Why are you interested in this role?</label><input id="field"></form></body></html>`)
	b := newTestBot(t)
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "seek", 0, "")
	scopeJobDependentFields(&snap, linkedInJob{ID: "123", Title: "Coordinator"})
	require.NotEmpty(t, snap.Fields[0].JobContext)
	result := fillAdaptiveControl(context.Background(), page, snap.Fields[0], "My reviewed answer")
	require.NoError(t, result.Err)
	assert.True(t, result.Filled)
}
