package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func linkedInPendingUploadFixture(mode string) string {
	return nativeDialogWithResume("library-resume.pdf", true) + fmt.Sprintf(`<script>
	window.uploadClicks=0;window.deliveries=0;
	const button=document.querySelector('#resume-section button');
	button.addEventListener('click',event=>{
	 if(!event.isTrusted)return;window.uploadClicks++;
	 const file=document.createElement('input');file.type='file';file.style.display='none';
	 file.addEventListener('change',()=>{
	  window.deliveries++;button.textContent='Uploading';button.setAttribute('aria-busy','true');
	  const filename=file.files[0].name;file.remove();
	  if(%q==='pending')return;
	  setTimeout(()=>{
	   button.textContent='Upload resume';button.removeAttribute('aria-busy');
	   setTimeout(()=>{
	    const radio=document.createElement('input');radio.type='radio';radio.name='resume-select';radio.id='accepted-resume';radio.checked=true;
	    const label=document.createElement('label');label.htmlFor=radio.id;label.textContent=%q==='accepted'?filename:'wrong-resume.pdf';
	    document.getElementById('resume-section').insertBefore(radio,button);document.getElementById('resume-section').insertBefore(label,button);
	   },200);
	  },800);
	 });
	 document.body.append(file);file.click();
	});
	</script>`, mode, mode)
}

func TestLinkedInAdaptiveDocument_PendingUploadIsNeverRedispatched(t *testing.T) {
	for _, mode := range []string{"accepted", "wrong", "pending"} {
		t.Run(mode, func(t *testing.T) {
			page := adaptiveControlTestPage(t, linkedInPendingUploadFixture(mode))
			b := New(Config{Platform: domain.PlatformLinkedIn})
			fields, _, err := b.scanAdaptiveStep(context.Background(), page)
			require.NoError(t, err)
			snap := captureStepSnapshot(fields, nil, "linkedin", 0, "Documents")
			var field SnapField
			for _, candidate := range snap.Fields {
				if candidate.Type == SnapTypeFile {
					field = candidate
					break
				}
			}
			require.NotEmpty(t, field.Ref)
			path := filepath.Join(t.TempDir(), "approved-resume.pdf")
			require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4 approved content"), 0o600))
			budget := 6 * time.Second
			if mode == "pending" {
				budget = 2 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			lazy := &lazyDocGen{b: b, ctx: ctx, frozen: true, packRestored: true, resume: path}
			calls := 0
			filler := &stubFillerFn{navOK: true, fillFn: func(ctx context.Context, field SnapField, _ FieldResolution) FieldFillResult {
				calls++
				return b.fillAdaptiveDocument(ctx, page, lazy, field)
			}}
			snap.Fields = []SnapField{field}
			interp := StepInterpretation{Fields: []FieldResolution{{Ref: field.Ref, Action: ActionUpload}}}
			started := time.Now()
			result, err := NewStepExecutor(DefaultStepExecBounds(), nil).ExecuteStep(ctx, snap, interp, filler)
			if mode == "accepted" {
				require.NoError(t, err)
				assert.Equal(t, 1, result.FieldsFilled)
				assert.Equal(t, StepOutcomeAdvanced, result.Outcome)
			} else {
				require.ErrorIs(t, err, errAdaptiveDocumentReview)
				if mode == "wrong" {
					require.ErrorIs(t, err, errUploadIdentityUnverifiable)
					assert.Equal(t, uploadStagePostUploadMismatch, lazy.holdStage)
					assert.Equal(t, "approved-resume.pdf", lazy.holdFile)
				}
				if mode == "pending" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			}
			assert.GreaterOrEqual(t, time.Since(started), 800*time.Millisecond, "old checked resume must not complete or fail an in-flight upload")
			assert.Equal(t, 1, calls, "production document errors must stop executor retries")
			counts, evalErr := page.Eval(`()=>[window.uploadClicks,window.deliveries]`)
			require.NoError(t, evalErr)
			assert.Equal(t, 1, counts.Value.Get("0").Int())
			assert.Equal(t, 1, counts.Value.Get("1").Int())
		})
	}
}

func TestLinkedInResumeAcceptance_PendingCannotReuseOldSelection(t *testing.T) {
	page := adaptiveControlTestPage(t, nativeDialogWithResume("approved-resume.pdf", true))
	_, err := page.Eval(`()=>{const button=document.querySelector('#resume-section button');button.textContent='Uploading';button.setAttribute('aria-busy','true');}`)
	require.NoError(t, err)
	for _, script := range []string{jsLinkedInCheckResumeAcceptedLite, jsLinkedInCheckResumeAccepted} {
		state, err := page.Eval(script, "approved-resume.pdf")
		require.NoError(t, err)
		assert.True(t, state.Value.Get("uploading").Bool())
		assert.False(t, state.Value.Get("accepted").Bool())
	}
}
