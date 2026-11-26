package llm

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
)

// RequestObservation deliberately contains no prompt, credentials or headers.
// The serialized byte bound includes image data and provider framing.
type RequestObservation struct {
	Provider             string
	Model                string
	OperationID          string
	Attempt              int
	PayloadBytes         int
	InputTokenUpperBound int64
	MaxOutputTokens      int
}

// RequestBudget is an optional per-operation guard. It runs for every actual
// HTTP attempt, including retries, and observes reported usage independently
// of the existing billing ledger and eval hooks.
type RequestBudget interface {
	BeforeRequest(context.Context, RequestObservation) error
	ObserveUsage(context.Context, UsageObservation) error
}
type requestBudgetKey struct{}
type requestImageBoundKey struct{}
type requestImageBound struct {
	encodedBytes int
	tokens       int64
}

// Claude consumes a native image, not base64 text tokens. Use a deliberately
// conservative area bound (one token per 64 pixels plus framing), exceeding
// the documented one-token-per-750-pixels image conversion. Invalid or oversized
// PNGs cannot consume a guarded request. Existing unguarded calls are unchanged.
func withRequestImageBound(ctx context.Context, imageBytes []byte) (context.Context, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 {
		return ctx, &nonRetryableError{err: fmt.Errorf("llm request budget: invalid or oversized PNG")}
	}
	area := int64(cfg.Width) * int64(cfg.Height)
	return context.WithValue(ctx, requestImageBoundKey{}, requestImageBound{encodedBytes: ((len(imageBytes) + 2) / 3) * 4, tokens: (area+63)/64 + 2048}), nil
}

func requestInputTokenBound(ctx context.Context, payloadBytes int) int64 {
	if image, ok := ctx.Value(requestImageBoundKey{}).(requestImageBound); ok {
		return int64(payloadBytes-image.encodedBytes) + image.tokens
	}
	return int64(payloadBytes)
}

func WithRequestBudget(ctx context.Context, budget RequestBudget) context.Context {
	return context.WithValue(ctx, requestBudgetKey{}, budget)
}
func requestBudgetFrom(ctx context.Context) RequestBudget {
	b, _ := ctx.Value(requestBudgetKey{}).(RequestBudget)
	return b
}

// WithOutputLimit returns a copy retaining credentials, quota, billing, policy
// and routing. A caller may lower the configured cap, never raise it.
func (c *Client) WithOutputLimit(limit int) *Client {
	if c == nil {
		return nil
	}
	nc := *c
	if limit > 0 && limit < nc.effectiveOutputLimit() {
		nc.cfg.MaxTokens = limit
	}
	return &nc
}

func (c *Client) effectiveOutputLimit() int {
	if c.cfg.MaxTokens > 0 {
		return c.cfg.MaxTokens
	}
	return 8192
}
