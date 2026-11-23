package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstrumentedUploadClick_WorksWhenAnimationFramesAreSuspended(t *testing.T) {
	page := rodTestPage(t, `<html><body><div role="dialog" class="jobs-easy-apply-modal"><button id="upload" style="width:180px;height:40px">Upload resume</button></div><script>window.requestAnimationFrame=()=>0;document.getElementById('upload').onclick=e=>{window.clicked=e.isTrusted;};</script></body></html>`)
	button, err := page.Element("#upload")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, newTestBot(t).instrumentedUploadClick(ctx, page, button, "resume.pdf"))
	clicked, err := page.Eval(`()=>!!window.clicked`)
	require.NoError(t, err)
	assert.True(t, clicked.Value.Bool())
}
