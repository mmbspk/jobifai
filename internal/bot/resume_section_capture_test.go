package bot

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// This fixture is the redacted upload dialog captured from the failing Talent
// preparation on Nov 3, 2026. Preserve its div wrappers and aria-label radios:
// replacing them with fieldsets/labels would hide the production regression.
func TestLinkedInResumeSection_CapturedDivLayout(t *testing.T) {
	html, err := os.ReadFile("testdata/linkedin/native-dialog-div-resume.html")
	require.NoError(t, err)
	page := rodTestPage(t, string(html))
	const group = "radio-group-_r_1v_"
	const intended = "resume_87b14c14.pdf"

	pre, err := page.Eval(jsLinkedInResumePreUploadState, intended)
	require.NoError(t, err)
	require.True(t, pre.Value.Get("sectionFound").Bool())
	require.True(t, pre.Value.Get("stateVerified").Bool())
	require.False(t, pre.Value.Get("filenameExisted").Bool())
	pre, err = page.Eval(jsLinkedInResumePreUploadState, "Resume.pdf")
	require.NoError(t, err)
	require.True(t, pre.Value.Get("filenameExisted").Bool(), "unchecked library entries count too")

	scan, err := page.Eval(jsScanFields)
	require.NoError(t, err)
	var fields []formField
	require.NoError(t, json.Unmarshal([]byte(scan.Value.String()), &fields))
	found := false
	for _, f := range fields {
		if f.UploadViaButton {
			found = true
			require.Equal(t, group, f.ResumeGroupName)
		}
	}
	require.True(t, found)
	checked, err := page.Eval(jsFindCheckedRadioLabel, group)
	require.NoError(t, err)
	require.Equal(t, "cover.pdf", checked.Value.String(), "aria-label supplies the filename")
	accepted, err := page.Eval(jsLinkedInCheckResumeAccepted, intended)
	require.NoError(t, err)
	require.False(t, accepted.Value.Get("filenameVisible").Bool())

	// Simulate the DOM state after successful file delivery, retaining all old
	// entries. This tests acceptance, not a file chooser or network upload.
	_, err = page.Eval(`(group, filename) => {
  const input = document.createElement('input');
  input.type = 'radio'; input.name = group; input.setAttribute('aria-label', filename);
  const old = [...document.querySelectorAll('input[type="radio"]')].find(r => r.name === group);
  old.parentElement.appendChild(input); input.checked = true;
 }`, group, intended)
	require.NoError(t, err)
	accepted, err = page.Eval(jsLinkedInCheckResumeAccepted, intended)
	require.NoError(t, err)
	require.True(t, accepted.Value.Get("accepted").Bool())
	require.True(t, accepted.Value.Get("filenameVisible").Bool())
	checked, err = page.Eval(jsFindCheckedRadioLabel, group)
	require.NoError(t, err)
	require.Equal(t, intended, checked.Value.String())
	pre, err = page.Eval(jsLinkedInResumePreUploadState, intended)
	require.NoError(t, err)
	require.True(t, pre.Value.Get("filenameExisted").Bool())
}

func TestLinkedInResumeSection_DivScopeIsolation(t *testing.T) {
	const html = `<dialog open><div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">
 <div id="auth"><input type="radio" name="work-auth" aria-label="Yes" checked></div>
 <div id="resume"><div><input type="radio" name="resume" aria-label="my_resume.pdf" checked></div>
 <div><button>Upload resume</button></div></div></div></dialog>`
	for _, tc := range []struct {
		name, mutate string
		verified     bool
	}{
		{"unrelated_group_outside_section", `() => {}`, true},
		{"unrelated_group_inside_section", `() => document.getElementById('resume').appendChild(document.getElementById('auth'))`, false},
		{"second_upload_button", `() => { const b = document.createElement('button'); b.textContent='Upload resume'; document.getElementById('resume').appendChild(b); }`, false},
		{"group_extends_outside_section", `() => { const r=document.createElement('input'); r.type='radio'; r.name='resume'; r.setAttribute('aria-label','outside.pdf'); document.getElementById('auth').appendChild(r); }`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, html)
			_, err := page.Eval(tc.mutate)
			require.NoError(t, err)
			pre, err := page.Eval(jsLinkedInResumePreUploadState, "my_resume.pdf")
			require.NoError(t, err)
			require.Equal(t, tc.verified, pre.Value.Get("stateVerified").Bool())
			if tc.verified {
				require.True(t, pre.Value.Get("filenameExisted").Bool())
			} else {
				accepted, err := page.Eval(jsLinkedInCheckResumeAccepted, "my_resume.pdf")
				require.NoError(t, err)
				require.False(t, accepted.Value.Get("accepted").Bool())
			}
		})
	}
}
