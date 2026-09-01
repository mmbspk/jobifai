package domain

import "time"

// Platform represents a supported job board.
type Platform string

const (
	PlatformLinkedIn Platform = "linkedin"
	PlatformSeek     Platform = "seek"
)

// SupportedPlatforms lists every platform the bot can run against.
// Add new platforms here, the rest of the system picks them up automatically.
var SupportedPlatforms = []Platform{PlatformLinkedIn, PlatformSeek}

// BotState represents the current state of the automation bot.
type BotState string

const (
	BotStateIdle          BotState = "idle"
	BotStateRunning       BotState = "running"
	BotStatePaused        BotState = "paused"
	BotStatePendingReview BotState = "pending_review"
	BotStateStopped       BotState = "stopped"
	BotStateError         BotState = "error"
)

// LoginMethod represents how a platform session was established.
type LoginMethod string

const (
	LoginMethodEmailPassword LoginMethod = "email_password"
	LoginMethodGoogleOAuth   LoginMethod = "google_oauth"
	LoginMethodProfileReuse  LoginMethod = "profile_reuse"
	LoginMethodManual        LoginMethod = "manual"
)

// ─── Auth ──────────────────────────────────────────────────────────────────

type PlatformSession struct {
	Platform    Platform    `json:"platform"`
	HasSession  bool        `json:"has_session"`
	LoginMethod LoginMethod `json:"login_method,omitempty"`
	CreatedAt   time.Time   `json:"created_at,omitempty"`
}

// ─── Jobs ──────────────────────────────────────────────────────────────────

type AppliedJob struct {
	ID               string        `json:"id"`
	Platform         Platform      `json:"platform"`
	Company          string        `json:"company"`
	Role             string        `json:"role"`
	Location         string        `json:"location,omitempty"`
	Link             string        `json:"link"`
	ResumePath       string        `json:"resume_path,omitempty"`
	CoverLetterPath  string        `json:"cover_letter_path,omitempty"`
	SuitabilityScore int           `json:"suitability_score,omitempty"`
	HalalVerdict     *HalalVerdict `json:"halal_verdict,omitempty"`
	AppliedAt        time.Time     `json:"applied_at"`
}

// HalalVerdict holds the result of an Islamic employment ethics evaluation.
type HalalVerdict struct {
	Verdict     string   `json:"verdict"`    // "HALAL" | "HARAM" | "DOUBTFUL"
	Confidence  string   `json:"confidence"` // "HIGH" | "MEDIUM" | "LOW"
	Summary     string   `json:"summary"`
	Reasons     []string `json:"reasons"`
	Caveats     *string  `json:"caveats"`
	ScholarNote *string  `json:"scholar_note"`
}

type SkippedJob struct {
	ID                   string        `json:"id"`
	Platform             Platform      `json:"platform"`
	Company              string        `json:"company"`
	Role                 string        `json:"role"`
	Location             string        `json:"location,omitempty"`
	Link                 string        `json:"link"`
	SkipReason           string        `json:"skip_reason"`
	SuitabilityScore     int           `json:"suitability_score,omitempty"`
	SuitabilityReasoning string        `json:"suitability_reasoning,omitempty"`
	HalalVerdict         *HalalVerdict `json:"halal_verdict,omitempty"`
	ViewedAt             time.Time     `json:"viewed_at"`
}

type PendingReview struct {
	JobID                string        `json:"job_id"`
	Company              string        `json:"company"`
	Role                 string        `json:"role"`
	Location             string        `json:"location,omitempty"`
	Platform             Platform      `json:"platform"`
	Link                 string        `json:"link,omitempty"`
	ResumePath           string        `json:"resume_path,omitempty"`
	CoverLetterPath      string        `json:"cover_letter_path,omitempty"`
	SuitabilityScore     int           `json:"suitability_score,omitempty"`
	SuitabilityReasoning string        `json:"suitability_reasoning,omitempty"`
	DueDate              string        `json:"due_date,omitempty"`
	PostedDate           string        `json:"posted_date,omitempty"`
	EasyApply            bool          `json:"easy_apply"`
	HalalVerdict         *HalalVerdict `json:"halal_verdict,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
}

type JobStats struct {
	TotalApplied    int `json:"total_applied"`
	AppliedToday    int `json:"applied_today"`
	TotalSkipped    int `json:"total_skipped"`
	SkippedToday    int `json:"skipped_today"`
	TopMatchesCount int `json:"top_matches_count"`
}

// JobScore is the result of an LLM suitability evaluation.
type JobScore struct {
	Score     int    `json:"score"`
	Reasoning string `json:"reasoning"`
}

// ─── Bot ───────────────────────────────────────────────────────────────────

type JobSummary struct {
	JobID    string   `json:"job_id"`
	Company  string   `json:"company"`
	Role     string   `json:"role"`
	Platform Platform `json:"platform"`
}

type BotStatus struct {
	State      BotState    `json:"state"`
	Platform   Platform    `json:"platform,omitempty"`
	Location   string      `json:"location,omitempty"`
	Keyword    string      `json:"keyword,omitempty"`
	StartedAt  *time.Time  `json:"started_at,omitempty"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
	Error      string      `json:"error,omitempty"`
	CurrentJob *JobSummary `json:"current_job,omitempty"`
	TodayCount int         `json:"today_count"`
	DailyLimit int         `json:"daily_limit"`
}

// ─── Resume Profile ────────────────────────────────────────────────────────

type PersonalInformation struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Surname     string `json:"surname,omitempty" yaml:"surname,omitempty"`
	Headline    string `json:"headline,omitempty" yaml:"headline,omitempty"`
	Email       string `json:"email,omitempty" yaml:"email,omitempty"`
	Phone       string `json:"phone,omitempty" yaml:"phone,omitempty"`
	PhonePrefix string `json:"phone_prefix,omitempty" yaml:"phone_prefix,omitempty"`
	Country     string `json:"country,omitempty" yaml:"country,omitempty"`
	City        string `json:"city,omitempty" yaml:"city,omitempty"`
	Address     string `json:"address,omitempty" yaml:"address,omitempty"`
	ZipCode     string `json:"zip_code,omitempty" yaml:"zip_code,omitempty"`
	GitHub      string `json:"github,omitempty" yaml:"github,omitempty"`
	LinkedIn    string `json:"linkedin,omitempty" yaml:"linkedin,omitempty"`
}

type EducationDetail struct {
	EducationLevel       string `json:"education_level,omitempty" yaml:"education_level,omitempty"`
	Institution          string `json:"institution,omitempty" yaml:"institution,omitempty"`
	FieldOfStudy         string `json:"field_of_study,omitempty" yaml:"field_of_study,omitempty"`
	Thesis               string `json:"thesis,omitempty" yaml:"thesis,omitempty"`
	FinalEvaluationGrade string `json:"final_evaluation_grade,omitempty" yaml:"final_evaluation_grade,omitempty"`
	StartDate            string `json:"start_date,omitempty" yaml:"start_date,omitempty"`
	YearOfCompletion     string `json:"year_of_completion,omitempty" yaml:"year_of_completion,omitempty"`
}

type Publication struct {
	Authors string `json:"authors,omitempty" yaml:"authors,omitempty"`
	Title   string `json:"title,omitempty" yaml:"title,omitempty"`
	Journal string `json:"journal,omitempty" yaml:"journal,omitempty"`
	Year    string `json:"year,omitempty" yaml:"year,omitempty"`
	DOI     string `json:"doi,omitempty" yaml:"doi,omitempty"`
	Status  string `json:"status,omitempty" yaml:"status,omitempty"`
}

type Presentation struct {
	Year       string `json:"year,omitempty" yaml:"year,omitempty"`
	Conference string `json:"conference,omitempty" yaml:"conference,omitempty"`
	Title      string `json:"title,omitempty" yaml:"title,omitempty"`
	Role       string `json:"role,omitempty" yaml:"role,omitempty"`
}

type Grant struct {
	Year    string `json:"year,omitempty" yaml:"year,omitempty"`
	Funder  string `json:"funder,omitempty" yaml:"funder,omitempty"`
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
	Amount  string `json:"amount,omitempty" yaml:"amount,omitempty"`
}

type ExperienceDetail struct {
	Position            string   `json:"position,omitempty" yaml:"position,omitempty"`
	Company             string   `json:"company,omitempty" yaml:"company,omitempty"`
	EmploymentPeriod    string   `json:"employment_period,omitempty" yaml:"employment_period,omitempty"`
	Location            string   `json:"location,omitempty" yaml:"location,omitempty"`
	Industry            string   `json:"industry,omitempty" yaml:"industry,omitempty"`
	KeyResponsibilities []string `json:"key_responsibilities,omitempty" yaml:"key_responsibilities,omitempty"`
	SkillsAcquired      []string `json:"skills_acquired,omitempty" yaml:"skills_acquired,omitempty"`
}

type Project struct {
	Name         string   `json:"name,omitempty" yaml:"name,omitempty"`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
	Link         string   `json:"link,omitempty" yaml:"link,omitempty"`
	Technologies []string `json:"technologies,omitempty" yaml:"technologies,omitempty"`
}

type Certification struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Issuer string `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	Date   string `json:"date,omitempty" yaml:"date,omitempty"`
	Link   string `json:"link,omitempty" yaml:"link,omitempty"`
}

type Language struct {
	Language    string `json:"language,omitempty" yaml:"language,omitempty"`
	Proficiency string `json:"proficiency,omitempty" yaml:"proficiency,omitempty"`
}

// ApplicationDefaults stores answers to common job-application form questions
// (visa sponsorship, work authorisation, notice period, etc.).
// These are used by the form-filler before falling back to LLM.
type ApplicationDefaults struct {
	RequiresSponsorship bool   `json:"requires_sponsorship,omitempty" yaml:"requires_sponsorship,omitempty"`
	NoticePeriod        string `json:"notice_period,omitempty" yaml:"notice_period,omitempty"`           // e.g. "2 weeks", "1 month", "Immediately"
	SalaryExpectation   string `json:"salary_expectation,omitempty" yaml:"salary_expectation,omitempty"` // e.g. "90000" or "90,000 AUD"
}

type ResumeProfile struct {
	PersonalInformation PersonalInformation `json:"personal_information,omitempty" yaml:"personal_information,omitempty"`
	EducationDetails    []EducationDetail   `json:"education_details,omitempty" yaml:"education_details,omitempty"`
	ExperienceDetails   []ExperienceDetail  `json:"experience_details,omitempty" yaml:"experience_details,omitempty"`
	Projects            []Project           `json:"projects,omitempty" yaml:"projects,omitempty"`
	Certifications      []Certification     `json:"certifications,omitempty" yaml:"certifications,omitempty"`
	Publications        []Publication       `json:"publications,omitempty" yaml:"publications,omitempty"`
	Presentations       []Presentation      `json:"presentations,omitempty" yaml:"presentations,omitempty"`
	Grants              []Grant             `json:"grants,omitempty" yaml:"grants,omitempty"`
	Languages           []Language          `json:"languages,omitempty" yaml:"languages,omitempty"`
	Skills              []string            `json:"skills,omitempty" yaml:"skills,omitempty"`
	Interests           []string            `json:"interests,omitempty" yaml:"interests,omitempty"`
	Summary             string              `json:"summary,omitempty" yaml:"summary,omitempty"`
	ApplicationDefaults ApplicationDefaults `json:"application_defaults,omitempty" yaml:"application_defaults,omitempty"`
	PromptInstructions  string              `json:"prompt_instructions,omitempty" yaml:"prompt_instructions,omitempty"`
}

// ─── Settings ──────────────────────────────────────────────────────────────

// TaskModel overrides the model and token limit for a specific LLM task.
// Keys: "scoring", "halal", "tailoring", "cover_letter", "form_filling".
type TaskModel struct {
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

type LLMConfig struct {
	Provider   string               `json:"provider"`
	Model      string               `json:"model"`
	UseProxy   bool                 `json:"use_proxy"`
	ProxyURL   string               `json:"proxy_url,omitempty"`
	MaxTokens  int                  `json:"max_tokens,omitempty"` // 0 = use model default (8192 for Claude)
	TaskModels map[string]TaskModel `json:"task_models,omitempty"`
}

type BrowserConfig struct {
	ShowBrowser       bool   `json:"show_browser"`
	UseChromeProfile  bool   `json:"use_chrome_profile"`
	ChromeProfilePath string `json:"chrome_profile_path,omitempty"`
	RemoteDebugPort   int    `json:"remote_debug_port,omitempty"`
}

type HumanBehaviorConfig struct {
	DailyApplicationLimit int     `json:"daily_application_limit,omitempty"`
	JobReadTimeMin        int     `json:"job_read_time_min,omitempty"`
	JobReadTimeMax        int     `json:"job_read_time_max,omitempty"`
	PauseBetweenJobsMin   int     `json:"pause_between_jobs_min,omitempty"`
	PauseBetweenJobsMax   int     `json:"pause_between_jobs_max,omitempty"`
	InteractionPauseMin   float64 `json:"interaction_pause_min,omitempty"`
	InteractionPauseMax   float64 `json:"interaction_pause_max,omitempty"`
	TypingSpeedMin        float64 `json:"typing_speed_min,omitempty"`
	TypingSpeedMax        float64 `json:"typing_speed_max,omitempty"`
	BusinessHoursStart    int     `json:"business_hours_start,omitempty"`
	BusinessHoursEnd      int     `json:"business_hours_end,omitempty"`
}

type GeneralSettings struct {
	LLM                        LLMConfig           `json:"llm,omitempty"`
	Browser                    BrowserConfig       `json:"browser,omitempty"`
	HumanBehavior              HumanBehaviorConfig `json:"human_behavior,omitempty"`
	DefaultResumeMarket        string              `json:"default_resume_market,omitempty"`
	RequireReview              bool                `json:"require_review_before_submit,omitempty"`
	JobSuitabilityScore        int                 `json:"job_suitability_score,omitempty"`
	MaxJobsPerKeyword          int                 `json:"max_jobs_per_keyword,omitempty"`
	HalalJobFilter             bool                `json:"halal_job_filter,omitempty"`
	GenerateNewResumeDocs      bool                `json:"generate_new_resume_docs,omitempty"`
	InterviewQuestionsEnabled  bool                `json:"interview_questions_enabled,omitempty"`
}

// AnswerQuestionsRequest is the payload for POST /api/resume/answer-questions.
type AnswerQuestionsRequest struct {
	JobURL      string   `json:"job_url"`
	JobDesc     string   `json:"job_description"`
	Questions   []string `json:"questions"`
}

// QuestionAnswer holds a single question and its LLM-generated answer.
type QuestionAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type ExperienceLevelConfig struct {
	Internship bool `json:"internship,omitempty"`
	Entry      bool `json:"entry,omitempty"`
	Associate  bool `json:"associate,omitempty"`
	MidSenior  bool `json:"mid_senior_level,omitempty"`
	Senior     bool `json:"senior,omitempty"`
	Director   bool `json:"director,omitempty"`
	Executive  bool `json:"executive,omitempty"`
}

type JobTypeConfig struct {
	FullTime   bool `json:"full_time,omitempty"`
	Contract   bool `json:"contract,omitempty"`
	PartTime   bool `json:"part_time,omitempty"`
	Temporary  bool `json:"temporary,omitempty"`
	Internship bool `json:"internship,omitempty"`
	Other      bool `json:"other,omitempty"`
	Volunteer  bool `json:"volunteer,omitempty"`
}

type DateFilterConfig struct {
	AllTime bool `json:"all_time,omitempty"`
	Month   bool `json:"month,omitempty"`
	Week    bool `json:"week,omitempty"`
	Hours24 bool `json:"hours_24,omitempty"`
}

// SearchTarget bundles a location with per-location work arrangement filters.
// Job titles (Positions) are shared across all targets.
type SearchTarget struct {
	Location string `json:"location,omitempty"`
	Remote   bool   `json:"remote,omitempty"`
	Hybrid   bool   `json:"hybrid,omitempty"`
	Onsite   bool   `json:"onsite,omitempty"`
}

type WorkPreferences struct {
	Remote             bool                  `json:"remote,omitempty"`
	Hybrid             bool                  `json:"hybrid,omitempty"`
	Onsite             bool                  `json:"onsite,omitempty"`
	ExperienceLevel    ExperienceLevelConfig `json:"experience_level,omitempty"`
	JobTypes           JobTypeConfig         `json:"job_types,omitempty"`
	Date               DateFilterConfig      `json:"date_filters,omitempty"`
	Positions          []string              `json:"positions,omitempty"`
	SearchTargets      []SearchTarget        `json:"search_targets,omitempty"`
	Locations          []string              `json:"locations,omitempty"`
	Distance           int                   `json:"distance,omitempty"`
	CompanyBlacklist   []string              `json:"company_blacklist,omitempty"`
	TitleBlacklist     []string              `json:"title_blacklist,omitempty"`
	LocationBlacklist  []string              `json:"location_blacklist,omitempty"`
}

type SecretsConfig struct {
	LLMAPIKey           string     `json:"llm_api_key,omitempty"`
	CredentialPlatforms []Platform `json:"credential_platforms,omitempty"`
}

type ResumeStyle struct {
	Name    string `json:"name"`
	CSSFile string `json:"css_file"`
}

type ResumeMarket struct {
	Name     string `json:"name"`
	YAMLFile string `json:"yaml_file"`
	HasCSS   bool   `json:"has_css"`
}

// SessionUsage holds LLM token usage accumulated during the current server session.
type SessionUsage struct {
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	Calls            int      `json:"calls"`
	EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
}
