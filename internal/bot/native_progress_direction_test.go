package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeNavigation_RequiresForwardProgress(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		for _, next := range []int{3, 1} {
			t.Run(fmt.Sprintf("%s/2-to-%d", platform, next), func(t *testing.T) {
				container := `<div role="dialog" class="jobs-easy-apply-modal">%s</div>`
				if platform == "seek" {
					container = `<form data-testid="quick-apply-form">%s</form>`
				}
				html := `<html><body>` + fmt.Sprintf(container, `<h2>Questions</h2><progress id="step" aria-valuenow="2" max="4"></progress><button type="button" id="next">Continue</button>`) + fmt.Sprintf(`<script>window.clicks=0;document.getElementById('next').onclick=()=>{window.clicks++;document.getElementById('step').setAttribute('aria-valuenow','%d');};</script></body></html>`, next)
				page := rodTestPage(t, html)
				var filler FieldFiller = &linkedInRodFiller{page: page, bot: newTestBot(t)}
				if platform == "seek" {
					filler = &seekRodFiller{page: page}
				}
				advanced, err := filler.NavigateNext(context.Background())
				assert.Equal(t, next > 2, advanced)
				if next > 2 {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				clicks, err := page.Eval(`()=>window.clicks`)
				require.NoError(t, err)
				assert.Equal(t, 1, clicks.Value.Int())
			})
		}
	}
}
