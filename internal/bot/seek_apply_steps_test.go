package bot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanFields_SeekFullPageApplyEmployerQuestions(t *testing.T) {
	t.Parallel()
	const jobID = "95395260"
	mux := http.NewServeMux()
	mux.HandleFunc("/job/"+jobID+"/apply/role-requirements", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><main>
<button data-testid="continue-button" type="button">Continue</button>
<textarea aria-label="Do you have any leave planned in the next 6 months?"></textarea>
<textarea aria-label="Do you have any prior criminal convictions or pending charges that we should be aware of?"></textarea>
</main></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/job/"+jobID+"/apply/role-requirements")
	fields := evalScanFields(t, page)
	var textareas int
	for _, f := range fields {
		if f.Type == "text" && f.InputType == "textarea" {
			textareas++
		}
	}
	require.GreaterOrEqual(t, textareas, 2, "full-page Seek apply must scan employer textareas")
}

func TestScanFields_SeekAriaRadioGroups(t *testing.T) {
	t.Parallel()
	const jobID = "95199897"
	mux := http.NewServeMux()
	mux.HandleFunc("/job/"+jobID+"/apply/role-requirements", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><main>
<p>Are you eligible to work in Australia?</p>
<div role="radiogroup" aria-labelledby="q1">
<span id="q1">Are you eligible to work in Australia?</span>
<div role="radio" aria-checked="false">Yes</div>
<div role="radio" aria-checked="false">No</div>
</div>
<button data-testid="continue-button" type="button">Continue</button>
</main></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/job/"+jobID+"/apply/role-requirements")
	fields := evalScanFields(t, page)
	var radios int
	for _, f := range fields {
		if f.Type == "radio" && len(f.Options) >= 2 {
			radios++
		}
	}
	require.GreaterOrEqual(t, radios, 1, "Seek ARIA radiogroups must be scanned as radio fields")
}

func TestScanFields_SeekListboxPlaceholder(t *testing.T) {
	t.Parallel()
	const jobID = "95199897"
	mux := http.NewServeMux()
	mux.HandleFunc("/job/"+jobID+"/apply/role-requirements", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><main>
<p>Which of the following statements best describes your right to work in Australia?</p>
<button type="button" aria-haspopup="listbox">Please make a selection</button>
<button data-testid="continue-button">Continue</button>
</main></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/job/"+jobID+"/apply/role-requirements")
	fields := evalScanFields(t, page)
	var found bool
	for _, f := range fields {
		if f.Type == "select" && f.SeekListbox {
			found = true
		}
	}
	require.True(t, found, "Seek listbox placeholders must scan as select fields")
}

func TestScanFields_SeekYesNoButtons(t *testing.T) {
	t.Parallel()
	const jobID = "95199897"
	mux := http.NewServeMux()
	mux.HandleFunc("/job/"+jobID+"/apply/role-requirements", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><main>
<section data-testid="employer-question">
<p>Do you have working rights in Australia?</p>
<button type="button">Yes</button>
<button type="button">No</button>
</section>
<button data-testid="continue-button">Continue</button>
</main></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/job/"+jobID+"/apply/role-requirements")
	fields := evalScanFields(t, page)
	require.NotEmpty(t, fields)
}

func TestSeekApplyPath_UsesPathname(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/job/123/apply/review", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><main><button data-testid="review-submit-application">Submit</button></main></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	page := navigateAndWait(t, launchTestBrowser(t), srv.URL+"/job/123/apply/review")
	assert.Equal(t, "/job/123/apply/review", seekApplyPath(page))
}
