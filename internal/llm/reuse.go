package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llmreuse"
)

type ReuseCoordinator struct {
	Store *llmreuse.Store
}

func messageParts(msgs []Message) []llmreuse.MessagePart {
	out := make([]llmreuse.MessagePart, len(msgs))
	for i, m := range msgs {
		out[i] = llmreuse.MessagePart{Role: m.Role, Content: m.Content}
	}
	return out
}

func reuseDefersValidation(task string) bool {
	switch task {
	case domain.TaskResumeTailoring, domain.TaskCoverLetter:
		return true
	default:
		return false
	}
}

func (c *Client) reuseBegin(ctx context.Context, msgs []Message) (llmreuse.BeginResult, string, string, error) {
	if c.reuse == nil || c.reuse.Store == nil || c.userID == "" {
		return llmreuse.BeginResult{}, "", "", nil
	}
	call := CallContextFrom(ctx)
	if call.BypassReuse {
		return llmreuse.BeginResult{}, "", "", nil
	}
	task := call.Task
	if task == "" {
		return llmreuse.BeginResult{}, "", "", nil
	}
	effort := c.taskRuntime.Effort
	fp := llmreuse.ContentFingerprint(c.userID, task, c.cfg.Provider, c.cfg.Model, effort, c.cfg.MaxTokens, messageParts(msgs))
	vis := llmreuse.VisualIdentityHash("", "", "")
	br, err := c.reuse.Store.Begin(ctx, c.userID, task, fp, vis, call.OperationID)
	if err != nil {
		return llmreuse.BeginResult{}, fp, task, fmt.Errorf("llm reuse begin: %w", err)
	}
	return br, fp, task, nil
}

// startReuseHeartbeat owns an immutable context and cancels the provider on lease loss.
func (c *Client) startReuseHeartbeat(parent context.Context, task, fp, owner string) (context.Context, func()) {
	return c.reuseHeartbeat(parent, task, fp, owner, 45*time.Second)
}

func (c *Client) reuseHeartbeat(parent context.Context, task, fp, owner string, interval time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, release := context.WithTimeout(ctx, 10*time.Second)
				err := c.reuse.Store.RenewLease(renewCtx, c.userID, task, fp, owner)
				release()
				if err != nil {
					cancel(fmt.Errorf("renew generation lease: %w", err))
					return
				}
			}
		}
	}()
	return ctx, func() { close(stop); <-done; cancel(context.Canceled) }
}
