package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestLinkedInEducation_CaptionVariationsRetainRecordOwnership(t *testing.T) {
	for _, captions := range []struct{ school, degree, subject string }{
		{"School", "Degree", "Field of study"},
		{"Institution", "Education Degree", "Major"},
		{"Institution name", "Qualification type", "Subject"},
	} {
		t.Run(captions.school, func(t *testing.T) {
			page := adaptiveControlTestPage(t, fmt.Sprintf(`<html><body><dialog open><div data-sdui-screen="EasyApply"><fieldset><legend>Education</legend><label for="school">%s*</label><input id="school" value="South College" required><label for="degree">%s*</label><select id="degree" required><option value="" disabled selected>Select</option><option>BS</option><option>MS</option></select><label for="study">%s</label><select id="study"><option value="" disabled selected>Select</option><option>Chemistry</option><option>Physics</option></select></fieldset></div></dialog></body></html>`, captions.school, captions.degree, captions.subject))
			b := newTestBot(t)
			b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "North College", EducationLevel: "MSc", FieldOfStudy: "Physics"}, {Institution: "South College", EducationLevel: "BSc", FieldOfStudy: "Chemistry"}}}
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
			delegate := &stubLLM{}
			result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, b.cfg.Profile)
			require.NoError(t, err)
			require.False(t, result.Paused, "%+v", result.Fields)
			require.Zero(t, delegate.calls)
			for _, f := range snap.Fields {
				require.Equal(t, "/education_details/1", f.ProfileRecordPath)
			}
			for _, r := range result.Fields {
				field, ok := snap.FieldByRef(r.Ref)
				require.True(t, ok)
				filled := (&linkedInRodFiller{page: page, bot: b}).Fill(context.Background(), field, r)
				require.NoError(t, filled.Err)
				require.True(t, filled.Filled)
			}
			values, err := page.Eval(`()=>[document.getElementById('degree').value,document.getElementById('study').value]`)
			require.NoError(t, err)
			require.Equal(t, "BS", values.Value.Get("0").String())
			require.Equal(t, "Chemistry", values.Value.Get("1").String())
		})
	}
}

func TestLinkedInEducation_SubjectOnlyRowKeepsInstitutionBinding(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><fieldset><label for="school">Institution</label><input id="school" value="South College"><label for="study">Field of study</label><select id="study"><option>Chemistry</option><option>Physics</option></select></fieldset></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "North College", FieldOfStudy: "Physics"}, {Institution: "South College", FieldOfStudy: "Chemistry"}}}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
	result, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, b.cfg.Profile)
	require.NoError(t, err)
	require.False(t, result.Paused)
	for _, f := range snap.Fields {
		require.Equal(t, "/education_details/1", f.ProfileRecordPath)
	}
	require.Equal(t, "Chemistry", resFor(t, result, "f0").Value)
}

func TestLinkedInEducation_UnfamiliarGroupedCaptionKeepsGroundingScope(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><fieldset><legend>Education</legend><label for="school">Institution</label><input id="school" value="South College"><label for="award">Academic award</label><select id="award"><option>BS</option><option>MS</option></select></fieldset></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "North College", EducationLevel: "MSc"}, {Institution: "South College", EducationLevel: "BSc"}}}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
	var award SnapField
	for _, f := range snap.Fields {
		require.Equal(t, "/education_details/1", f.ProfileRecordPath)
		if f.ControlID == "award" {
			award = f
		}
	}
	require.NotEmpty(t, award.Ref)
	wrong := LLMFieldProposal{Ref: award.Ref, Action: string(ActionFill), IntentType: FieldIntentLLM, Value: "MS", Evidence: []ProfileEvidence{{Path: "/education_details/0/education_level", Quote: "MSc"}}}
	_, ok := prepareGroundedCandidate(b.cfg.Profile, snap, &wrong)
	require.False(t, ok, "model cannot borrow the other institution's qualification")
	correct := LLMFieldProposal{Ref: award.Ref, Action: string(ActionFill), IntentType: FieldIntentLLM, Value: "BS", Evidence: []ProfileEvidence{{Path: "/education_details/1/education_level", Quote: "BSc"}}}
	_, ok = prepareGroundedCandidate(b.cfg.Profile, snap, &correct)
	require.True(t, ok, "the novel caption can proceed to independent verification")
}

func TestLinkedInEducation_UnownedCaptionVariationHolds(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><label for="school">Institution</label><input id="school" value="South College"><label for="award">Academic award</label><select id="award"><option>BS</option><option>MS</option></select></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "North College", EducationLevel: "MSc"}, {Institution: "South College", EducationLevel: "BSc"}}}
	_, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
}

func TestLinkedInEducation_ConflictingCurrentAttendanceHolds(t *testing.T) {
	for _, mutation := range []string{"before scan", "during fill"} {
		t.Run(mutation, func(t *testing.T) {
			page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><fieldset><label for="school">Institution</label><input id="school" value="South College"><label for="degree">Degree</label><select id="degree"><option value="" disabled selected>Select</option><option>BS</option><option>MS</option></select><div><input id="attend" type="checkbox"><span>I currently attend this institution</span></div></fieldset></div></dialog></body></html>`)
			b := newTestBot(t)
			b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc", YearOfCompletion: "December 2010"}}}
			if mutation == "before scan" {
				_, err := page.Eval(`()=>document.getElementById('attend').checked=true`)
				require.NoError(t, err)
				_, _, err = b.scanAdaptiveStep(context.Background(), page)
				require.ErrorIs(t, err, errAdaptiveQuestionReview)
				require.Contains(t, err.Error(), "current-attendance")
				return
			}
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
			var degree SnapField
			for _, f := range snap.Fields {
				if f.ControlID == "degree" {
					degree = f
				}
			}
			require.NotEmpty(t, degree.ProfileRecordPath)
			_, err = page.Eval(`()=>document.getElementById('degree').addEventListener('change',()=>document.getElementById('attend').checked=true)`)
			require.NoError(t, err)
			filled := (&linkedInRodFiller{page: page, bot: b}).Fill(context.Background(), degree, FieldResolution{Action: ActionFill, Value: "BS"})
			require.False(t, filled.Filled)
			require.ErrorIs(t, filled.Err, errAdaptiveQuestionReview)
			require.Contains(t, filled.Err.Error(), "current-attendance")
		})
	}
}

func TestLinkedInEducation_AmbiguousCaptionControlsHold(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><fieldset><label for="school">Institution</label><input id="school" value="South College"><label for="degree1">Degree</label><select id="degree1"><option>BS</option><option>MS</option></select><label for="degree2">Qualification</label><select id="degree2"><option>BS</option><option>MS</option></select></fieldset></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc"}}}
	_, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.Contains(t, err.Error(), "ambiguous degree or subject")
}

func TestLinkedInEducation_CompletionRequiresAnUnambiguousPastDate(t *testing.T) {
	for _, date := range []string{"2010", "December 2010", "2010-12-01"} {
		require.True(t, educationCompleted(domain.EducationDetail{YearOfCompletion: date}))
	}
	for _, date := range []string{"", "present", "2010-present", "Expected 2010", "2006-2010", "2099"} {
		require.False(t, educationCompleted(domain.EducationDetail{YearOfCompletion: date}))
	}
}

func TestLinkedInForm_MaterializesLateEducationAndBindsOneRecord(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><div id="column" data-testid="lazy-column" style="height:180px;overflow:auto"><div style="height:220px"><label for="school">School</label><input id="school" aria-label="School" value="South College" required></div><div id="late" style="height:400px"></div></div><button>Next</button></div></dialog><script>const column=document.getElementById('column');column.addEventListener('scroll',()=>{if(column.scrollTop>0&&!document.getElementById('degree'))document.getElementById('late').innerHTML='<label for="degree">Education Degree*</label><select id="degree" required><option value="" disabled selected>Select an option</option><option>MS</option><option>BS</option></select><label for="study">Education Field of Study</label><select id="study"><option value="" disabled selected>Select an option</option><option>Chemistry</option><option>Physics</option></select>';});</script></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "North College", EducationLevel: "MSc", FieldOfStudy: "Physics"}, {Institution: "South College", EducationLevel: "BSc", FieldOfStudy: "Chemistry"}}}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	require.Len(t, fields, 3)
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
	delegate := &stubLLM{}
	result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, b.cfg.Profile)
	require.NoError(t, err)
	require.False(t, result.Paused, "%+v", result.Fields)
	require.Zero(t, delegate.calls)
	for _, f := range snap.Fields {
		require.Equal(t, "/education_details/1", f.ProfileRecordPath)
	}
	for _, r := range result.Fields {
		field, ok := snap.FieldByRef(r.Ref)
		require.True(t, ok)
		filled := (&linkedInRodFiller{page: page, bot: b}).Fill(context.Background(), field, r)
		require.NoError(t, filled.Err)
		require.True(t, filled.Filled)
	}
	values, err := page.Eval(`()=>[document.getElementById('school').value,document.getElementById('degree').value,document.getElementById('study').value]`)
	require.NoError(t, err)
	require.Equal(t, "South College", values.Value.Get("0").String())
	require.Equal(t, "BS", values.Value.Get("1").String())
	require.Equal(t, "Chemistry", values.Value.Get("2").String())
}

func TestLinkedInEducation_RecordEvidenceAndReuseStayScoped(t *testing.T) {
	a := SnapField{Ref: "f0", Type: SnapTypeSelect, Label: "Education Degree", ProfileRecordPath: "/education_details/0", Options: []string{"BS", "MS"}}
	b := a
	b.ProfileRecordPath = "/education_details/1"
	require.NotEqual(t, a.FieldSignature("linkedin"), b.FieldSignature("linkedin"))
	require.True(t, educationEvidenceCompatible(b, []ProfileEvidence{{Path: "/education_details/1/education_level", Quote: "BSc"}}))
	require.False(t, educationEvidenceCompatible(b, []ProfileEvidence{{Path: "/education_details/0/education_level", Quote: "MSc"}}))
	require.False(t, educationEvidenceCompatible(b, []ProfileEvidence{{Path: "/summary", Quote: "Master degree"}}))
}

func TestLinkedInEducation_LocationSuffixNeedsMatchingDates(t *testing.T) {
	record := domain.EducationDetail{Institution: "South College", StartDate: "September 2006", YearOfCompletion: "2010"}
	row := educationFormRow{Institution: "South College, Southern City", StartYear: "2006", EndYear: "2010"}
	require.True(t, educationRowMatches(row, record))
	row.EndYear = "2017"
	require.False(t, educationRowMatches(row, record))
	row.EndYear = ""
	require.False(t, educationRowMatches(row, record))
	row.Institution = "Different College, Southern City"
	row.EndYear = "2010"
	require.False(t, educationRowMatches(row, record))
	row.Institution = "South College"
	row.StartYear = "2012"
	require.False(t, educationRowMatches(row, record))
}

func TestLinkedInEducation_LocationSuffixUsesOwnedDateControls(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><label for="school">School</label><input id="school" aria-label="School" value="South College, Southern City" required><div><p>From</p><div><select id="start"><option value="">Year</option><option value="2006" selected>2006</option><option value="2012">2012</option></select></div></div><div><p>To</p><div><select id="end"><option value="">Year</option><option value="2010" selected>2010</option><option value="2017">2017</option></select></div></div><div><input id="attend" type="checkbox"><span>I currently attend this institution</span></div><label for="degree">Education Degree*</label><select id="degree" required><option value="" disabled selected>Select</option><option>BS</option><option>MS</option></select></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc", StartDate: "September 2006", YearOfCompletion: "2010"}}}
	fields, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.NoError(t, err)
	snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
	result, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, b.cfg.Profile)
	require.NoError(t, err)
	require.False(t, result.Paused, "%+v", result.Fields)
	for _, r := range result.Fields {
		field, ok := snap.FieldByRef(r.Ref)
		require.True(t, ok)
		filled := (&linkedInRodFiller{page: page, bot: b}).Fill(context.Background(), field, r)
		require.NoError(t, filled.Err)
		require.True(t, filled.Filled || filled.Skipped)
	}
	values, err := page.Eval(`()=>[document.getElementById('degree').value,document.getElementById('attend').checked]`)
	require.NoError(t, err)
	require.Equal(t, "BS", values.Value.Get("0").String())
	require.False(t, values.Value.Get("1").Bool())
}

func TestLinkedInEducation_AmbiguousInstitutionHolds(t *testing.T) {
	page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><label for="school">School</label><input id="school" aria-label="School" value="South College"><label for="degree">Education Degree*</label><select id="degree" required><option>BS</option><option>MS</option></select></div></dialog></body></html>`)
	b := newTestBot(t)
	b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "MSc"}, {Institution: "South College", EducationLevel: "BSc"}}}
	_, _, err := b.scanAdaptiveStep(context.Background(), page)
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
}

func TestLinkedInEducation_RecordCannotChangeAroundAssignment(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before fill", true: "during fill"}[after], func(t *testing.T) {
			page := adaptiveControlTestPage(t, `<html><body><dialog open><div data-sdui-screen="EasyApply"><label for="school">School</label><input id="school" aria-label="School" value="South College"><label for="degree">Education Degree*</label><select id="degree" required><option value="" disabled selected>Select</option><option>BS</option><option>MS</option></select></div></dialog></body></html>`)
			b := newTestBot(t)
			b.cfg.Profile = &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc"}}}
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Education")
			var degree SnapField
			for _, f := range snap.Fields {
				if f.ControlID == "degree" {
					degree = f
				}
			}
			require.NotEmpty(t, degree.ProfileRecordPath)
			mutation := `()=>document.getElementById('school').value='Other College'`
			if after {
				mutation = `()=>document.getElementById('degree').addEventListener('change',()=>document.getElementById('school').value='Other College')`
			}
			_, err = page.Eval(mutation)
			require.NoError(t, err)
			result := (&linkedInRodFiller{page: page, bot: b}).Fill(context.Background(), degree, FieldResolution{Action: ActionFill, Value: "BS"})
			require.ErrorIs(t, result.Err, errAdaptiveQuestionReview)
			require.False(t, result.Filled)
		})
	}
}
