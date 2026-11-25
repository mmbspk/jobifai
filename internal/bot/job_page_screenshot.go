package bot

import (
	"context"
	"github.com/user/jobifai/internal/jobpage"
)

func (p *capturedJobPage) ScreenshotFor(ctx context.Context, target jobpage.Target, snapshot jobpage.Snapshot, limits jobpage.Limits) ([]byte, error) {
	if capture, ok := p.Page.(interface {
		ScreenshotFor(context.Context, jobpage.Target, jobpage.Snapshot, jobpage.Limits) ([]byte, error)
	}); ok {
		return capture.ScreenshotFor(ctx, target, snapshot, limits)
	}
	return nil, nil
}
