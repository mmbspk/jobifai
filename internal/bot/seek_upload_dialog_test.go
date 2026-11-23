package bot

import (
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"testing"
)

func TestSeekResumeUpload_ActivationDetectsLimitWithoutDelivery(t *testing.T) {
	page := rodTestPage(t, `<html><body><main data-testid="quick-apply-form"><div data-testid="resumeFileInput"><button id="upload">Upload</button><input id="resume" type="file" hidden></div><p data-testid="10-resume-limit-text" id="limit" hidden>Resumé limit reached</p><p id="saved">Existing résumé</p><input id="default" type="checkbox"><button>Continue</button></main><script>document.getElementById('upload').onclick=()=>document.getElementById('limit').hidden=false;</script></body></html>`)
	b := New(Config{Platform: domain.PlatformSeek})
	modern, err := b.seekActivateResumeUpload(page)
	require.True(t, modern)
	require.ErrorIs(t, err, errAdaptiveDocumentReview)
	state, err := page.Eval(`()=>[document.getElementById('resume').files.length,!!document.getElementById('saved'),document.getElementById('default').checked]`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("0").Int())
	require.True(t, state.Value.Get("1").Bool())
	require.False(t, state.Value.Get("2").Bool())
}

func TestSeekResumeUpload_ChooserCannotRedirectToCover(t *testing.T) {
	page := rodTestPage(t, `<html><body><main data-testid="quick-apply-form"><div data-testid="resumeFileInput"><button id="upload">Upload</button><input id="resume" type="file" hidden></div><div data-testid="coverLetterFileInput"><input id="cover" type="file" hidden></div><button>Continue</button></main><script>document.getElementById('upload').onclick=()=>document.getElementById('cover').click();</script></body></html>`)
	modern, err := New(Config{Platform: domain.PlatformSeek}).seekActivateResumeUpload(page)
	require.True(t, modern)
	require.ErrorIs(t, err, errAdaptiveDocumentReview)
	state, err := page.Eval(`()=>[document.getElementById('resume').files.length,document.getElementById('cover').files.length]`)
	require.NoError(t, err)
	require.Zero(t, state.Value.Get("0").Int())
	require.Zero(t, state.Value.Get("1").Int())
}
