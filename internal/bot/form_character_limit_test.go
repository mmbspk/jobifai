package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormCharacterLimits_ScopesCounterToItsControl(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		limits     []int
	}{
		{"page progress and contact controls", `<div><p>1/4 pages</p><section><label for=mail>Email address</label><select id=mail><option>a@example.test</option></select><label for=phone>Mobile phone number</label><div><input id=phone type=tel value=0412345678></div></section></div>`, []int{0, 0}},
		{"separate nearby counter", `<div><div><label for=short>Answer</label><input id=short value=abc><span>3/20</span></div><div><label for=phone>Phone</label><input id=phone type=tel value=0412345678></div></div>`, []int{20, 0}},
		{"explicit description", `<div><label for=answer>Answer</label><textarea id=answer aria-describedby=counter>abc</textarea></div><span id=counter>3 of 60 characters</span>`, []int{60}},
		{"date in question", `<div><label for=answer>Available from 1/4?</label><input id=answer value=Immediately></div>`, []int{0}},
		{"local character counter", `<div><label for=answer>Answer</label><textarea id=answer>abc</textarea><span>3 of 80 characters</span></div>`, []int{80}},
		{"native maximum", `<div><label for=answer>Answer</label><input id=answer maxlength=12 value=abc><span>3/80</span></div>`, []int{12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, `<!doctype html><html><body><dialog open><div data-sdui-screen="com.linkedin.sdui.flagshipnav.jobs.easyapply.EasyApply">`+tc.body+`</div></dialog></body></html>`)
			fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredFields": true})
			require.Len(t, fields, len(tc.limits))
			for i, want := range tc.limits {
				require.Equal(t, want, fields[i].MaxChars)
			}
			// The post-navigation validation scanner must use the same ownership
			// rule, so an unrelated counter cannot trigger a corrective rewrite.
			b := &Bot{}
			violations, err := b.linkedInTextLimitViolations(page, false)
			require.NoError(t, err)
			require.Empty(t, violations)
		})
	}
}
