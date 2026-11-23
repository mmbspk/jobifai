package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestSeekDocuments_SelectionRequiresOneExactScopedResume(t *testing.T) {
	for _, tc := range []struct {
		name, options string
		selected      bool
	}{
		{"new upload retains old selection", `<input id="approved" type="radio" name="document-select-current"><label for="approved">approved.pdf</label>`, true},
		{"different filename", `<input id="approved" type="radio" name="document-select-current"><label for="approved">other.pdf</label>`, false},
		{"substring filename", `<input id="approved" type="radio" name="document-select-current"><label for="approved">unapproved.pdf</label>`, false},
		{"duplicate filename", `<input id="approved" type="radio" name="document-select-current"><label for="approved">approved.pdf</label><input id="duplicate" type="radio" name="document-select-current"><label for="duplicate">approved.pdf</label>`, false},
		{"wrong group", `<input id="approved" type="radio" name="coverLetter-method-current"><label for="approved">approved.pdf</label>`, false},
		{"disabled entry", `<input id="approved" type="radio" name="document-select-current" disabled><label for="approved">approved.pdf</label>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, fmt.Sprintf(`<html><body><header><input id="decoy" type="radio" name="document-select-decoy"><label for="decoy">approved.pdf</label></header><main data-testid="quick-apply-form"><h1>Choose documents</h1><section><div data-testid="resumeFileInput"><input type="file"></div><input id="old" type="radio" name="document-select-current" checked><label for="old">old.pdf</label>%s<input id="default" type="checkbox"><label for="default">Make this my default</label></section><button data-testid="continue-button">Continue</button></main><script>window.clicks=0;for(const radio of document.querySelectorAll('input[type=radio]'))radio.addEventListener('click',()=>window.clicks++);</script></body></html>`, tc.options))
			result, err := page.Eval(jsSeekSelectApprovedResume, "approved.pdf")
			require.NoError(t, err)
			assert.Equal(t, tc.selected, result.Value.Bool())
			checks, err := page.Eval(`()=>[document.getElementById('old').checked,document.getElementById('decoy').checked,document.getElementById('default').checked,window.clicks]`)
			require.NoError(t, err)
			assert.Equal(t, !tc.selected, checks.Value.Get("0").Bool())
			assert.False(t, checks.Value.Get("1").Bool())
			assert.False(t, checks.Value.Get("2").Bool())
			if tc.selected {
				assert.Equal(t, 1, checks.Value.Get("3").Int())
			} else {
				assert.Zero(t, checks.Value.Get("3").Int())
			}
			if !tc.selected && !strings.Contains(tc.name, "duplicate") {
				state, err := page.Eval(jsSeekDocumentSelected, "resume", "approved.pdf")
				require.NoError(t, err)
				assert.False(t, state.Value.Get("selected").Bool())
			}
		})
	}
}

func TestSeekDocuments_HiddenNativeInputsAreScopedAndSelected(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string {
		return `<html><body><header><div data-testid="resumeFileInput"><input id="decoy" type="file"></div></header><main><h1>Choose documents</h1><input id="utility" type="file" style="display:none"><section><div data-testid="resumeFileInput"><button id="resume-upload">Upload</button><input id="resume-fileFile" type="file" accept=".pdf" style="display:none"></div><div id="library"><fieldset style="display:contents"><input id="old" type="radio" name="document-select-fixture" checked><label for="old">old-resume.pdf</label></fieldset></div></section><section><div data-testid="coverLetterFileInput"><button>Upload</button><input id="coverLetter-fileFile" type="file" accept=".pdf" style="display:none"><span id="cover-name"></span></div><label><input id="none" type="radio" name="coverLetter-method-fixture" data-testid="coverLetter-method-none" checked>Don't include a cover letter</label></section><button data-testid="continue-button">Continue</button></main><script>document.getElementById('resume-upload').onclick=()=>document.getElementById('resume-fileFile').click();document.getElementById('resume-fileFile').onchange=e=>{const r=document.createElement('input');r.type='radio';r.name='document-select-fixture';r.id='uploaded';r.checked=false;const l=document.createElement('label');l.htmlFor='uploaded';l.textContent=e.target.files[0].name;document.getElementById('library').append(r,l);};document.getElementById('coverLetter-fileFile').onchange=e=>{document.getElementById('none').checked=false;document.getElementById('cover-name').textContent=e.target.files[0].name;};</script></body></html>`
	})
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	b := New(Config{Platform: domain.PlatformSeek})
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	require.Len(t, fields, 2)
	caps := probeApplyFormCaps(page)
	assert.Equal(t, 1, caps.ResumeFileSlots)
	assert.True(t, caps.CoverOptional)
	dir := t.TempDir()
	resume, cover := filepath.Join(dir, "approved-resume.pdf"), filepath.Join(dir, "approved-cover.pdf")
	require.NoError(t, os.WriteFile(resume, []byte("%PDF-1.4 resume"), 0o600))
	require.NoError(t, os.WriteFile(cover, []byte("%PDF-1.4 cover"), 0o600))
	lazy := &lazyDocGen{b: b, ctx: context.Background(), frozen: true, packRestored: true, resume: resume, cover: cover}
	snap := captureStepSnapshot(fields, nil, "seek", 0, "Documents")
	for _, field := range snap.Fields {
		result := b.fillAdaptiveDocument(context.Background(), page, lazy, field)
		require.NoError(t, result.Err)
		assert.True(t, result.Filled)
	}
	values, err := page.Eval(`()=>[document.getElementById('resume-fileFile').files[0].name,document.getElementById('coverLetter-fileFile').files[0].name,document.getElementById('decoy').files.length,document.getElementById('utility').files.length]`)
	require.NoError(t, err)
	assert.Equal(t, "approved-resume.pdf", values.Value.Get("0").String())
	assert.Equal(t, "approved-cover.pdf", values.Value.Get("1").String())
	assert.Zero(t, values.Value.Get("2").Int())
	assert.Zero(t, values.Value.Get("3").Int())
}

type attachmentHoldFiller struct {
	stubFiller
	calls int
}

func (f *attachmentHoldFiller) Fill(context.Context, SnapField, FieldResolution) FieldFillResult {
	f.calls++
	return FieldFillResult{Err: errAdaptiveDocumentReview}
}

func TestStepExecutor_UncertainAttachmentIsNotUploadedAgain(t *testing.T) {
	filler := &attachmentHoldFiller{}
	snap := StepSnapshot{Fields: []SnapField{{Ref: "f0", Type: SnapTypeFile}}}
	interp := StepInterpretation{Fields: []FieldResolution{{Ref: "f0", Action: ActionUpload}}}
	_, err := NewStepExecutor(DefaultStepExecBounds(), nil).ExecuteStep(context.Background(), snap, interp, filler)
	require.ErrorIs(t, err, errAdaptiveDocumentReview)
	assert.Equal(t, 1, filler.calls)
}

func TestStepExecutor_DocumentHoldStopsRemainingFields(t *testing.T) {
	filler := &attachmentHoldFiller{}
	snap := StepSnapshot{Fields: []SnapField{{Ref: "f0", Type: SnapTypeFile}, {Ref: "f1", Type: SnapTypeFile}}}
	interp := StepInterpretation{Fields: []FieldResolution{{Ref: "f0", Action: ActionUpload}, {Ref: "f1", Action: ActionUpload}}}
	result, err := NewStepExecutor(DefaultStepExecBounds(), nil).ExecuteStep(context.Background(), snap, interp, filler)
	require.ErrorIs(t, err, errAdaptiveDocumentReview)
	assert.Equal(t, 1, filler.calls)
	assert.Equal(t, []string{"f0"}, result.FieldsFailed)
}

func TestAdaptiveScan_DecodeFailureIsNotAnEmptyStep(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="file">Resume</label><input id="file" type="file" accept=".pdf"></form><script>Object.defineProperty(document.getElementById('file'),'required',{get:()=> 'yes'});</script></body></html>`)
	fields, _, err := newTestBot(t).scanAdaptiveStep(context.Background(), page)
	require.Error(t, err)
	assert.Nil(t, fields)
	assert.Contains(t, err.Error(), "decode current form controls")
}

func TestAdaptiveScan_AssociatedCheckboxLabelDoesNotHideTheStep(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="resume">Resume</label><input id="resume" type="file" accept=".pdf"><fieldset><legend>Preferred shifts</legend><input id="day" name="shift" type="checkbox"><label for="day">Day shift</label></fieldset></form></body></html>`)
	fields, _, err := newTestBot(t).scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	require.Len(t, fields, 2)
	assert.Equal(t, "file", fields[0].Type)
	assert.Equal(t, "checkbox", fields[1].Type)
	assert.Equal(t, "Day shift", fields[1].Options[0].Label)
}

func TestAdaptiveScan_LeavesJobsiteDefaultResumeSettingAlone(t *testing.T) {
	page := rodTestPage(t, `<html><body><form data-testid="quick-apply-form"><label for="resume">Resume</label><input id="resume" type="file" accept=".pdf"><input id="default" type="checkbox"><label for="default">Make this my default resumé</label></form></body></html>`)
	fields, _, err := newTestBot(t).scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, "file", fields[0].Type)
	checked, err := page.Eval(`()=>document.getElementById('default').checked`)
	require.NoError(t, err)
	assert.False(t, checked.Value.Bool())
}

func seekFinalReviewFixture() string {
	return `<html><body><main><h1>Review application</h1><section id=resume-summary><div><h2>Resumé</h2></div><div><p id=resume-name>approved-resume.pdf</p><button aria-label="Download approved-resume.pdf">Download</button></div></section><section id=cover-summary><div><h2>Cover letter</h2></div><div><p id=cover-name>approved-cover.pdf</p></div></section><button id=submit data-testid=review-submit-application onclick="window.submits=(window.submits||0)+1">Submit application</button></main></body></html>`
}

func seekPlainFinalReviewFixture() string {
	return `<html><body><main><h1>Review application</h1><div id=document-summary><h3>Documents included</h3><div><div id=resume-summary><div><div><span>Resumé</span><span id=resume-name data-testid=resume-upload>approved-resume.pdf</span></div><div><button aria-label="Download approved-resume.pdf">Download</button></div></div></div><span></span><div id=cover-summary><div><div><span>Cover letter</span><span id=cover-name data-testid=cover-letter-upload>approved-cover.pdf</span></div><div></div></div></div></div></div><section><h3>Employer questions</h3><p id=other-filename>other.pdf</p></section><button id=submit data-testid=review-submit-application onclick="window.submits=(window.submits||0)+1">Submit application</button></main></body></html>`
}

func TestSeekFinalReviewDocuments_PlainDocumentRowsKeepSectionOwnership(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return seekPlainFinalReviewFixture() })
	lazy := &lazyDocGen{resume: "/approved/approved-resume.pdf", cover: "/approved/approved-cover.pdf"}
	for _, tc := range []struct {
		name, mutation string
		valid          bool
	}{
		{"plain document labels", `()=>{}`, true},
		{"nested label typography", `()=>document.querySelector('#resume-summary span').innerHTML='<strong>Résumé</strong>'`, true},
		{"wrong current resume", `()=>document.getElementById('resume-name').textContent='old.pdf'`, false},
		{"wrong current cover", `()=>document.getElementById('cover-name').textContent='old-cover.pdf'`, false},
		{"duplicate document row", `()=>{const row=document.getElementById('resume-summary');row.after(row.cloneNode(true));}`, false},
		{"duplicate document region", `()=>{const region=document.getElementById('document-summary');region.after(region.cloneNode(true));}`, false},
		{"ambiguous attachment names", `()=>document.getElementById('resume-name').after(Object.assign(document.createElement('span'),{textContent:'other.pdf'}))`, false},
		{"hidden approved resume", `()=>{document.getElementById('resume-name').hidden=true;document.querySelector('#resume-summary button').hidden=true;}`, false},
		{"document marker kind changed", `()=>document.getElementById('resume-name').setAttribute('data-testid','cover-letter-upload')`, false},
		{"unqualified plain labels", `()=>document.querySelector('#document-summary h3').textContent='Other information'`, false},
		{"adjacent section cannot supply filename", `()=>{document.getElementById('resume-name').remove();document.querySelector('#resume-summary button').remove();document.getElementById('other-filename').textContent='approved-resume.pdf';}`, false},
		{"neighbouring cover cannot supply resume", `()=>{document.getElementById('resume-name').remove();document.querySelector('#resume-summary button').remove();document.getElementById('cover-summary').insertAdjacentHTML('beforeend','<span>approved-resume.pdf</span>');}`, false},
		{"plain matching row outside region", `()=>{document.getElementById('resume-name').textContent='wrong.pdf';document.querySelector('main').insertAdjacentHTML('beforeend','<div><span>Resumé</span><span>approved-resume.pdf</span></div>');}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
			_, err := page.Eval(tc.mutation)
			require.NoError(t, err)
			err = seekVerifyFinalReviewDocuments(context.Background(), page, lazy)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errAdaptiveDocumentReview)
			}
		})
	}
}

func TestSeekFinalReviewDocuments_RequiresCurrentScopedAttachmentIdentities(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return seekFinalReviewFixture() })
	lazy := &lazyDocGen{resume: "/approved/approved-resume.pdf", cover: "/approved/approved-cover.pdf"}
	for _, tc := range []struct {
		name, mutation string
		valid          bool
	}{
		{"approved documents", `()=>{}`, true},
		{"wrong resume", `()=>{document.getElementById('resume-name').textContent='old.pdf';document.querySelector('#resume-summary button').remove();}`, false},
		{"missing cover", `()=>document.getElementById('cover-name').remove()`, false},
		{"wrong cover", `()=>document.getElementById('cover-name').textContent='old-cover.pdf'`, false},
		{"multiple resume filenames", `()=>document.getElementById('resume-summary').insertAdjacentHTML('beforeend','<p>other.pdf</p>')`, false},
		{"duplicate resume summary", `()=>{const section=document.getElementById('resume-summary');section.after(section.cloneNode(true));}`, false},
		{"matching resume outside application", `()=>{document.getElementById('resume-name').textContent='wrong.pdf';document.querySelector('#resume-summary button').remove();document.body.insertAdjacentHTML('beforeend','<aside><h2>Resumé</h2><p>approved-resume.pdf</p></aside>');}`, false},
		{"hidden correct attachment", `()=>{document.getElementById('resume-name').hidden=true;document.querySelector('#resume-summary button').hidden=true;document.getElementById('resume-summary').insertAdjacentHTML('beforeend','<p>old.pdf</p>');}`, false},
		{"download label evidence", `()=>document.getElementById('resume-name').remove()`, true},
		{"filename under wrong document heading", `()=>{document.getElementById('cover-summary').remove();document.getElementById('resume-summary').insertAdjacentHTML('beforeend','<p>approved-cover.pdf</p>');}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
			_, err := page.Eval(tc.mutation)
			require.NoError(t, err)
			err = seekVerifyFinalReviewDocuments(context.Background(), page, lazy)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errAdaptiveDocumentReview)
			}
		})
	}
}

func TestSeekFinalReviewDocuments_HoldsBeforeSubmitIntent(t *testing.T) {
	br := workerFixtureBrowser(t, func(string) string { return seekPlainFinalReviewFixture() })
	page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
	_, err := page.Eval(`()=>document.getElementById('cover-name').textContent='other.pdf'`)
	require.NoError(t, err)
	db := openMappingDB(t)
	ac := newAttemptContext("fixture-user", "seek", "12345678", "https://au.seek.com/job/12345678", "Coordinator", "Acme", "fixture", "fixture-run")
	beginAttempt(db, ac)
	filler := &seekRodFiller{page: page, lazy: &lazyDocGen{resume: "approved-resume.pdf", cover: "approved-cover.pdf"}}
	guard := &SubmitGuard{DB: db, AttemptID: ac.AttemptID, UserID: ac.UserID}
	_, err = NewStepExecutor(DefaultStepExecBounds(), guard).ExecuteStep(context.Background(), StepSnapshot{}, StepInterpretation{}, filler)
	require.ErrorIs(t, err, errAdaptiveDocumentReview)
	var stage string
	require.NoError(t, db.QueryRow(`SELECT stage FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&stage))
	assert.NotEqual(t, "submit_dispatch", stage)
	submits, err := page.Eval(`()=>window.submits||0`)
	require.NoError(t, err)
	assert.Zero(t, submits.Value.Int())
}

func TestSeekFinalReviewDocuments_RechecksImmediatelyBeforeSubmitClick(t *testing.T) {
	for _, layout := range []string{"headings", "plain rows"} {
		for _, mode := range []string{"unchanged", "hover replaces resume", "last guard replaces cover"} {
			t.Run(layout+"/"+mode, func(t *testing.T) {
				br := workerFixtureBrowser(t, func(string) string {
					if layout == "plain rows" {
						return seekPlainFinalReviewFixture()
					}
					return seekFinalReviewFixture()
				})
				page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
				filler := &seekRodFiller{page: page, lazy: &lazyDocGen{resume: "approved-resume.pdf", cover: "approved-cover.pdf"}}
				ready, err := filler.IsSubmitReady(context.Background())
				require.NoError(t, err)
				require.True(t, ready)
				if mode == "hover replaces resume" {
					_, err := page.Eval(`()=>document.getElementById('submit').onmouseover=()=>{document.getElementById('resume-name').textContent='replaced-resume.pdf';document.querySelector('#resume-summary button').setAttribute('aria-label','Download replaced-resume.pdf');}`)
					require.NoError(t, err)
				}
				if mode == "last guard replaces cover" {
					filler.beforeClick = func(context.Context) error {
						_, err := page.Eval(`()=>document.getElementById('cover-name').textContent='other.pdf'`)
						return err
					}
				}
				err = filler.ClickSubmit(context.Background())
				if mode == "unchanged" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, errAdaptiveDocumentReview)
				}
				submits, err := page.Eval(`()=>window.submits||0`)
				require.NoError(t, err)
				assert.Equal(t, map[bool]int{true: 1, false: 0}[mode == "unchanged"], submits.Value.Int())
			})
		}
	}
}

func TestSeekResumeLibraryLimit_NeverDeletesSavedDocuments(t *testing.T) {
	for _, filename := range []string{"bot-resume.pdf", "personal-document.docx"} {
		t.Run(filename, func(t *testing.T) {
			br := workerFixtureBrowser(t, func(string) string {
				return `<html><body><div role=dialog><p data-testid=10-resume-limit-text>Resumé limit reached</p><select id=docLimitExceededDropdown onchange="window.changed=true"><option value=first>Keep this document.pdf</option><option value=last>` + filename + `</option></select><button data-automation=10-resume-delete onclick="window.deleted=true">Delete</button></div></body></html>`
			})
			page := navigateAndWait(t, br, "https://au.seek.com/job/12345678/apply")
			err := seekHandleResumeLimitDialog(page)
			require.ErrorIs(t, err, errAdaptiveDocumentReview)
			state, err := page.Eval(`()=>({deleted:!!window.deleted,changed:!!window.changed,selected:document.getElementById('docLimitExceededDropdown').value})`)
			require.NoError(t, err)
			assert.False(t, state.Value.Get("deleted").Bool())
			assert.False(t, state.Value.Get("changed").Bool())
			assert.Equal(t, "first", state.Value.Get("selected").String())
			_, err = page.Eval(`()=>document.querySelector('[role=dialog]').hidden=true`)
			require.NoError(t, err)
			require.NoError(t, seekHandleResumeLimitDialog(page))
		})
	}
}
