package bot

import (
	"context"
	"testing"

	"github.com/go-rod/rod/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedMouse_CancelledContextCannotDispatchInput(t *testing.T) {
	page := rodTestPage(t, `<html><body><button style="width:200px;height:80px">Submit application</button><script>window.inputs=0;for(const event of ['mousemove','mousedown','mouseup','click'])document.addEventListener(event,()=>window.inputs++);</script></body></html>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	point := proto.Point{X: 50, Y: 30}
	require.ErrorIs(t, moveBoundedMouse(ctx, page, point), context.Canceled)
	require.ErrorIs(t, clickBoundedMouse(ctx, page, point), context.Canceled)
	inputs, err := page.Eval(`()=>window.inputs`)
	require.NoError(t, err)
	assert.Zero(t, inputs.Value.Int())
}
