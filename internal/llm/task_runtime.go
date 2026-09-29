package llm

// TaskRuntime holds per-task options resolved from settings or policy.
type TaskRuntime struct {
	Effort         string
	Mode           string
	MaxCostUSD     float64
	TimeoutSec     int
	FallbackModels []string // stored for eval/admin; production auto-fallback disabled
}
