package runmeta

const (
	RunnerFake = "fake"
	RunnerReal = "real"

	PurposeSmoke     = "smoke"
	PurposeBenchmark = "benchmark"

	StatusPending         = "pending"
	StatusRunning         = "running"
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusCancelled       = "cancelled"
	StatusBudgetExhausted = "budget_exhausted"
)

// MinBenchmarkCases returns minimum cases required for deployable recommendations per task.
func MinBenchmarkCases(task string) int {
	switch task {
	case "job_scoring":
		return 40
	case "employment_ethics":
		return 25
	case "form_answer":
		return 30
	case "form_vision":
		return 15
	case "resume_extract", "resume_tailoring", "cover_letter":
		return 15
	case "application_questions":
		return 20
	default:
		return 30
	}
}
