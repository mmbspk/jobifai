package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"strings"
	"time"
)

const jsSeekResumeUploadButton = `()=>{` + jsLinkedInApplyContainerFn + `
 const scope=resolveApplyContainer();if(!scope)return null;
 const slots=allInDOM(scope,'[data-testid=resumeFileInput]');if(slots.length!==1)return null;
 const buttons=allInDOM(slots[0],'button').filter(el=>{const r=el.getBoundingClientRect();return r.width>2&&r.height>2&&!el.disabled&&el.getAttribute('aria-disabled')!=='true'&&/^upload$/i.test((el.textContent||'').trim());});
 return buttons.length===1?buttons[0]:null;
}`

// Activate the modern upload action before delivering a file. SEEK may refuse
// the action with a library-limit dialog; setting the hidden input directly
// bypasses that UI and never establishes a successful upload.
func (b *Bot) seekActivateResumeUpload(page *rod.Page, approvedPaths ...string) (bool, error) {
	modern, err := b.seekActivateResumeUploadOnce(page)
	if err == nil || !errors.Is(err, errSeekResumeLibraryFull) || len(approvedPaths) != 1 || strings.TrimSpace(approvedPaths[0]) == "" {
		return modern, err
	}
	if _, recoveryErr := b.seekRecoverResumeLibrary(page, approvedPaths[0]); recoveryErr != nil {
		return modern, recoveryErr
	}
	// One capacity recovery per upload. A repeated full dialog is held rather
	// than deleting further documents while an earlier action is uncertain.
	return b.seekActivateResumeUploadOnce(page)
}

func (b *Bot) seekActivateResumeUploadOnce(page *rod.Page) (bool, error) {
	probe, err := page.Timeout(3 * time.Second).Eval(`()=>!!(` + jsSeekResumeUploadButton + `)()`)
	if err != nil {
		return false, err
	}
	if !probe.Value.Bool() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(page.GetContext(), 12*time.Second)
	defer cancel()
	page = page.Context(ctx)
	var chooser proto.PageFileChooserOpened
	wait := page.WaitEvent(&chooser)
	if err = (proto.PageSetInterceptFileChooserDialog{Enabled: true}).Call(page); err != nil {
		return true, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = (proto.PageSetInterceptFileChooserDialog{Enabled: false}).Call(page.Context(cleanup))
	}()
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	button, err := page.ElementByJS(rod.Eval(jsSeekResumeUploadButton))
	if err != nil {
		return true, err
	}
	if _, err = button.Eval(`()=>this.scrollIntoView({block:'nearest',behavior:'instant'})`); err != nil {
		return true, err
	}
	point, err := adaptiveTypeaheadClickPoint(ctx, page, button)
	if err != nil {
		return true, err
	}
	if err = moveBoundedMouse(ctx, page, *point); err != nil {
		return true, err
	}
	same, err := button.Eval(`()=>this===(` + jsSeekResumeUploadButton + `)()`)
	if err != nil || !same.Value.Bool() {
		return true, fmt.Errorf("%w: resume upload action changed", errAdaptiveDocumentReview)
	}
	point, err = adaptiveTypeaheadClickPoint(ctx, page, button)
	if err != nil {
		return true, err
	}
	if err = clickBoundedMouse(ctx, page, *point); err != nil {
		return true, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = seekHandleResumeLimitDialog(page); err != nil {
			return true, err
		}
		select {
		case <-ctx.Done():
			return true, fmt.Errorf("%w: resume upload action did not open a verified chooser", errAdaptiveDocumentReview)
		case <-done:
			if ctx.Err() != nil {
				return true, ctx.Err()
			}
			input, inputErr := seekDocumentInput(page, "resume")
			if inputErr != nil {
				return true, inputErr
			}
			node, nodeErr := (proto.DOMDescribeNode{ObjectID: input.Object.ObjectID}).Call(page)
			if nodeErr != nil {
				return true, nodeErr
			}
			if node.Node.BackendNodeID != chooser.BackendNodeID {
				return true, fmt.Errorf("%w: chooser does not belong to the current resume slot", errAdaptiveDocumentReview)
			}
			return true, nil
		case <-ticker.C:
		}
	}
}
