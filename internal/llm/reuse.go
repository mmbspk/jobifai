package llm

import (
	"context"

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

func (c *Client) reuseBegin(ctx context.Context, msgs []Message) (llmreuse.BeginResult, string, string, bool) {
	if c.reuse == nil || c.reuse.Store == nil || c.userID == "" {
		return llmreuse.BeginResult{}, "", "", false
	}
	call := CallContextFrom(ctx)
	task := call.Task
	if task == "" {
		return llmreuse.BeginResult{}, "", "", false
	}
	fp := llmreuse.ContentFingerprint(c.userID, task, messageParts(msgs))
	vis := llmreuse.VisualIdentityHash("", "", "")
	br, err := c.reuse.Store.Begin(ctx, c.userID, task, fp, vis, call.OperationID)
	if err != nil {
		return llmreuse.BeginResult{}, fp, task, false
	}
	return br, fp, task, true
}

func (c *Client) reuseComplete(ctx context.Context, task, fp, response string) {
	if c.reuse == nil || c.reuse.Store == nil {
		return
	}
	_ = c.reuse.Store.Complete(ctx, c.userID, task, fp, response)
}

func (c *Client) reuseFailedUncertain(ctx context.Context, task, fp string) {
	if c.reuse == nil || c.reuse.Store == nil {
		return
	}
	_ = c.reuse.Store.MarkFailedUncertain(ctx, c.userID, task, fp)
}
