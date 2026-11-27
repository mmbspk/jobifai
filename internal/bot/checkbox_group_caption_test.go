package bot

import (
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func checkboxCaptionCard(name, caption string, options ...string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "<div class='question-card'><div class='caption'><span>%s</span></div>", html.EscapeString(caption))
	for index, option := range options {
		fmt.Fprintf(&out, "<div class='choice'><div><input type='checkbox' id='%s-%d' name='%s' style='opacity:0' value='on'><label for='%s-%d'>%s</label></div></div>", name, index, name, name, index, html.EscapeString(option))
	}
	out.WriteString("</div>")
	return out.String()
}

func TestCheckboxGroupCaption_NativeCardsExposeQuestionInsteadOfFirstOption(t *testing.T) {
	const frameworks = "Which of the following front end development libraries and frameworks are you proficient in?"
	const databases = "Which of the following Relational Database Management Systems (RDBMS) are you experienced with?"
	body := checkboxCaptionCard("questionnaire.frameworks", frameworks, "jQuery", "React.js", "Node.js", "None of these") + checkboxCaptionCard("questionnaire.databases", databases, "MySQL", "Microsoft SQL", "PostgreSQL", "SQLite", "None of these")
	page := rodTestPage(t, "<html><body><main><h1>Application questions</h1><form data-testid='quick-apply-form'>"+body+"</form></main></body></html>")
	fields := evalScanFieldsOpts(t, page, map[string]interface{}{"includeAnsweredFields": true})
	require.Len(t, fields, 2)
	require.Equal(t, frameworks, fields[0].Question)
	require.Equal(t, databases, fields[1].Question)
	require.Len(t, fields[0].Options, 4)
	require.Equal(t, "jQuery", fields[0].Options[0].Label)
	require.Equal(t, "React.js", fields[0].Options[1].Label)
	require.Len(t, fields[1].Options, 5)
	require.Equal(t, "MySQL", fields[1].Options[0].Label)
	require.Equal(t, "PostgreSQL", fields[1].Options[2].Label)
}

func TestCheckboxGroupCaption_OwnershipAndExistingLegendsRemainRequired(t *testing.T) {
	for _, variation := range []string{"hidden caption", "multiple captions", "foreign controls"} {
		t.Run(variation, func(t *testing.T) {
			body := checkboxCaptionCard("frameworks", "Which frameworks are you proficient in?", "jQuery", "React.js")
			switch variation {
			case "hidden caption":
				body = strings.Replace(body, "class='caption'", "class='caption' hidden", 1)
			case "multiple captions":
				body = strings.Replace(body, "<div class='caption'>", "<div>Only one year of commercial experience qualifies.</div><div class='caption'>", 1)
			case "foreign controls":
				body = strings.Replace(body, "<div class='caption'>", "<input type='text' aria-label='Different question'><div class='caption'>", 1)
			}
			page := rodTestPage(t, "<html><body><form data-testid='quick-apply-form'>"+body+"</form></body></html>")
			fields := evalScanFields(t, page)
			require.NotEmpty(t, fields)
			require.NotEqual(t, "Which frameworks are you proficient in?", fields[0].Question, "an incomplete or unowned caption must not activate a reusable answer")
		})
	}
	page := rodTestPage(t, "<html><body><form data-testid='quick-apply-form'><fieldset><legend>Which databases are you experienced with?</legend><label><input name='db' type='checkbox'>MySQL</label><label><input name='db' type='checkbox'>PostgreSQL</label></fieldset></form></body></html>")
	fields := evalScanFields(t, page)
	require.Len(t, fields, 1)
	require.Equal(t, "Which databases are you experienced with?", fields[0].Question)
}
