package llm

import (
	"context"
	"fmt"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llmreuse"
)

type ReuseCoordinator struct {
	Store *llmreuse.Store
}

type pendingReuse struct {
	task, fp, lease string
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

func (c *Client) reuseComplete(ctx context.Context, task, fp, leaseOwner, response string) {
	if c.reuse == nil || c.reuse.Store == nil || leaseOwner == "" {
		return
	}
	_ = c.reuse.Store.Complete(ctx, c.userID, task, fp, leaseOwner, response)
}

func (c *Client) reuseFailedUncertain(ctx context.Context, task, fp, leaseOwner string) {
	if c.reuse == nil || c.reuse.Store == nil || leaseOwner == "" {
		return
	}
	_ = c.reuse.Store.MarkFailedUncertain(ctx, c.userID, task, fp, leaseOwner)
}

func (c *Client) reuseRenew(ctx context.Context, task, fp, leaseOwner string) {
	if c.reuse == nil || c.reuse.Store == nil || leaseOwner == "" {
		return
	}
	_ = c.reuse.Store.RenewLease(ctx, c.userID, task, fp, leaseOwner)
}

// CommitValidatedReuse completes cache after downstream validation (document tasks).
func (c *Client) CommitValidatedReuse(ctx context.Context, validatedResponse string) error {
	c.reusePending.mu.Lock()
	p := c.reusePending.p
	c.reusePending.p = nil
	c.reusePending.mu.Unlock()
	if p == nil || c.reuse == nil || c.reuse.Store == nil {
		return nil
	}
	return c.reuse.Store.Complete(ctx, c.userID, p.task, p.fp, p.lease, validatedResponse)
}

// AbortReuse releases a pending generation lease so callers can retry.
func (c *Client) AbortReuse(ctx context.Context) {
	c.reusePending.mu.Lock()
	p := c.reusePending.p
	c.reusePending.p = nil
	c.reusePending.mu.Unlock()
	if p == nil {
		return
	}
	c.reuseFailedUncertain(ctx, p.task, p.fp, p.lease)
}
