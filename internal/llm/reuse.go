package llm

import (
	"context"
	"fmt"

	"github.com/user/jobifai/internal/llmreuse"
)

// ReuseCoordinator wraps generation cache lookup for Chat.
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
	fp := llmreuse.ContentFingerprint(c.userID, task, c.cfg.Provider, c.cfg.Model, c.cfg.MaxTokens, messageParts(msgs))
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
