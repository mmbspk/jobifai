package bot

import (
	"context"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Call CDP on the bounded page directly. Rod's shared Mouse retains the context
// of its original page when Page.Context creates a clone.
func moveBoundedMouse(ctx context.Context, page *rod.Page, point proto.Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return (proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMouseMoved, X: point.X, Y: point.Y, Button: proto.InputMouseButtonNone}).Call(page.Context(ctx))
}

func clickBoundedMouse(ctx context.Context, page *rod.Page, point proto.Point) error {
	pressed, released := 1, 0
	for _, event := range []proto.InputDispatchMouseEvent{
		{Type: proto.InputDispatchMouseEventTypeMousePressed, X: point.X, Y: point.Y, Button: proto.InputMouseButtonLeft, Buttons: &pressed, ClickCount: 1},
		{Type: proto.InputDispatchMouseEventTypeMouseReleased, X: point.X, Y: point.Y, Button: proto.InputMouseButtonLeft, Buttons: &released, ClickCount: 1},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := event.Call(page.Context(ctx)); err != nil {
			return err
		}
	}
	return nil
}
