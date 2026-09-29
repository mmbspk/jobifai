package llm

import (
	"context"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
)

type callCtxKey struct{}

// WithCallContext attaches structured billing metadata to ctx.
func WithCallContext(ctx context.Context, c domain.LLMCallContext) context.Context {
	if c.CorrelationID == "" {
		c.CorrelationID = uuid.NewString()
	}
	if c.Task != "" {
		c.Task = domain.LegacyToStableTask(c.Task)
	}
	return context.WithValue(ctx, callCtxKey{}, c)
}

// CallContextFrom returns metadata from ctx or zero value.
func CallContextFrom(ctx context.Context) domain.LLMCallContext {
	if v, ok := ctx.Value(callCtxKey{}).(domain.LLMCallContext); ok {
		return v
	}
	// Legacy string task label
	if label, ok := ctx.Value(ctxKey{}).(string); ok && label != "" {
		return domain.LLMCallContext{Task: domain.LegacyToStableTask(label), CorrelationID: uuid.NewString()}
	}
	return domain.LLMCallContext{CorrelationID: uuid.NewString()}
}

// AttachAttribution merges job/run attribution without changing the task label.
func AttachAttribution(ctx context.Context, attr domain.LLMCallContext) context.Context {
	c := CallContextFrom(ctx)
	if attr.UserID != "" {
		c.UserID = attr.UserID
	}
	if attr.JobID != "" {
		c.JobID = attr.JobID
	}
	if attr.ApplicationID != "" {
		c.ApplicationID = attr.ApplicationID
	}
	if attr.AutomationRunID != "" {
		c.AutomationRunID = attr.AutomationRunID
	}
	if attr.OperationID != "" {
		c.OperationID = attr.OperationID
	}
	return WithCallContext(ctx, c)
}

// WithTask sets a legacy log label and stable task on the context.
func WithTask(ctx context.Context, label string) context.Context {
	c := CallContextFrom(ctx)
	c.Task = domain.LegacyToStableTask(label)
	if c.CorrelationID == "" {
		c.CorrelationID = uuid.NewString()
	}
	ctx = context.WithValue(ctx, callCtxKey{}, c)
	return context.WithValue(ctx, ctxKey{}, label)
}
