package quota

import "context"

// LLMGuard is the surface used by the LLM client and usage persistence hook.
type LLMGuard interface {
	BeforeLLM(ctx context.Context, userID, model string, estInput, estOutput int) error
	RecordLLM(ctx context.Context, userID, model string, input, output int64) error
}
