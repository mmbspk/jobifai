package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTryGeometryUploadClick_FullyCoveredButtonFails(t *testing.T) {
	t.Parallel()
	const html = `<!DOCTYPE html><html><head><style>
#cover { position: fixed; left: 0; top: 0; width: 100vw; height: 100vh; z-index: 9999; background: rgba(0,0,0,0.01); }
#btn { position: fixed; left: 50px; top: 50px; width: 120px; height: 40px; }
</style></head><body>
<div id="cover"></div>
<button id="btn" type="button" onclick="window._clicked=true">Upload resume</button>
</body></html>`

	br := launchTestBrowser(t)
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.SetDocumentContent(html))

	btn, err := page.Element("#btn")
	require.NoError(t, err)

	b := &Bot{cfg: Config{}}
	err = b.tryGeometryUploadClick(context.Background(), page, btn, "resume.pdf", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "covered")

	clicked, evalErr := page.Eval(`() => !!window._clicked`)
	require.NoError(t, evalErr)
	require.False(t, clicked.Value.Bool(), "covered upload button must not be clicked")
}
