package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Use bounded trusted input for the same native submit action after hover.
// An owned CSS decoration may be the CDP hit node; unrelated covers still hold.
// No iteration repeats a mouse press, even when the page replaces its control.
func clickLinkedInAdaptiveSubmit(ctx context.Context, page *rod.Page, verify func(context.Context) error) error {
	return clickLinkedInAdaptiveSubmitWithIntent(ctx, page, verify, nil)
}

func clickLinkedInAdaptiveSubmitWithIntent(ctx context.Context, page *rod.Page, verify func(context.Context) error, persistIntent func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	page = page.Context(ctx)
	if err := (proto.TargetActivateTarget{TargetID: page.TargetID}).Call(page.Browser().Context(ctx)); err != nil {
		return err
	}
	for refresh := 0; refresh < 3; refresh++ {
		ready, err := page.Eval(jsLinkedInAdaptiveSubmit, false)
		if err != nil {
			return err
		}
		if ready == nil || !ready.Value.Bool() {
			return fmt.Errorf("adaptive: submit control is missing or ambiguous")
		}
		button, err := page.ElementByJS(rod.Eval(jsLinkedInAdaptiveSubmit, true))
		if err != nil {
			return err
		}
		if _, err = button.Eval(`()=>this.scrollIntoView({block:'center',behavior:'instant'})`); err != nil {
			return err
		}
		point, err := linkedInNativeClickPoint(ctx, button)
		if page.IsIframe() {
			point, err = button.Context(ctx).Interactable()
		}
		if err != nil {
			return err
		}
		if err = moveBoundedMouse(ctx, page, *point); err != nil {
			return err
		}
		if verify != nil {
			if err = verify(ctx); err != nil {
				return err
			}
		}
		fresh, err := button.Eval(`()=>this===(` + jsLinkedInAdaptiveSubmit + `)(true)`)
		if err != nil {
			return err
		}
		if fresh == nil || !fresh.Value.Bool() {
			continue
		}
		point, err = linkedInNativeClickPoint(ctx, button)
		if page.IsIframe() {
			point, err = button.Context(ctx).Interactable()
		}
		if err != nil {
			return err
		}
		if verify != nil {
			if err = verify(ctx); err != nil {
				return err
			}
		}
		if !page.IsIframe() {
			point, err = probeLinkedInNativeClickPoint(ctx, button)
			if err != nil {
				continue
			}
		}
		fresh, err = button.Eval(`()=>this===(` + jsLinkedInAdaptiveSubmit + `)(true)`)
		if err != nil {
			return err
		}
		if fresh == nil || !fresh.Value.Bool() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if persistIntent != nil {
			if err := persistIntent(); err != nil {
				return err
			}
			// The durable write can wait on SQLite. Recheck dispatch ownership
			// and facts afterwards, but never retry or clear its intent.
			if verify != nil {
				if err := verify(ctx); err != nil {
					return err
				}
			}
			if !page.IsIframe() {
				point, err = probeLinkedInNativeClickPoint(ctx, button)
			} else {
				point, err = button.Context(ctx).Interactable()
			}
			if err != nil {
				return err
			}
			fresh, err = button.Eval(`()=>this===(` + jsLinkedInAdaptiveSubmit + `)(true)`)
			if err != nil {
				return err
			}
			if fresh == nil || !fresh.Value.Bool() {
				return fmt.Errorf("adaptive: submit control changed during intent persistence")
			}
		}
		return clickBoundedMouse(ctx, page, *point)
	}
	return fmt.Errorf("adaptive: submit control kept changing before dispatch")
}
