package domain

// Stable machine identifiers for LLM billing and policy.
const (
	TaskResumeExtract         = "resume_extract"
	TaskJobScoring            = "job_scoring"
	TaskEmploymentEthics      = "employment_ethics"
	TaskResumeTailoring       = "resume_tailoring"
	TaskCoverLetter           = "cover_letter"
	TaskFormAnswer            = "form_answer"
	TaskFormVision            = "form_vision"
	TaskApplicationQuestions  = "application_questions"
)

// LegacyTaskModelKeys are keys stored in general_settings.llm.task_models JSON.
var LegacyTaskModelKeys = []string{
	"scoring", "halal", "tailoring", "cover_letter", "form_filling", "questions",
}

// LegacyToStableTask maps legacy settings keys and log labels to stable task IDs.
func LegacyToStableTask(key string) string {
	switch key {
	case "scoring", "evaluate job":
		return TaskJobScoring
	case "halal", "halal check":
		return TaskEmploymentEthics
	case "tailoring", "tailor resume":
		return TaskResumeTailoring
	case "cover_letter", "cover letter":
		return TaskCoverLetter
	case "form_filling", "form question":
		return TaskFormAnswer
	case "identify form fields", "form_vision":
		return TaskFormVision
	case "questions", "answer questions":
		return TaskApplicationQuestions
	case "extract resume", "resume_extract":
		return TaskResumeExtract
	default:
		if key != "" {
			return key
		}
		return "unknown"
	}
}

// StableToLegacyTaskModelKey maps stable task IDs to legacy task_models map keys.
func StableToLegacyTaskModelKey(task string) string {
	switch task {
	case TaskJobScoring:
		return "scoring"
	case TaskEmploymentEthics:
		return "halal"
	case TaskResumeTailoring:
		return "tailoring"
	case TaskCoverLetter:
		return "cover_letter"
	case TaskFormAnswer, TaskFormVision:
		return "form_filling"
	case TaskApplicationQuestions:
		return "questions"
	case TaskResumeExtract:
		return "scoring" // no legacy key; resolver uses global
	default:
		return task
	}
}
