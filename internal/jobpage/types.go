// Package jobpage recovers verified listing content. It has no application
// control, candidate-profile, credential-change or submission interface.
package jobpage

import (
	"context"
	"errors"
	"time"

	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
)

const SnapshotVersion = 1
const RecipeVersion = 1

type Target struct {
	Platform  string `json:"platform"`
	URL       string `json:"url"`
	JobID     string `json:"job_id"`
	Title     string `json:"title,omitempty"`
	Company   string `json:"company,omitempty"`
	UserID    string `json:"-"`
	RunID     string `json:"-"`
	AttemptID string `json:"-"`
	RecordID  string `json:"-"` // Application DB ID may differ from the platform listing ID.
}

type Scope struct {
	Platform        string `json:"platform"`
	PageType        string `json:"page_type"`
	Origin          string `json:"origin"`
	SnapshotVersion int    `json:"snapshot_version"`
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Identity struct {
	JobID      string   `json:"job_id"`
	Title      string   `json:"title"`
	Company    string   `json:"company"`
	Verified   bool     `json:"verified"`
	HeaderRefs []string `json:"header_refs"`
	Evidence   []string `json:"evidence"`
}

type NodeContext string

const (
	ContextHeader      NodeContext = "header"
	ContextDescription NodeContext = "description"
	ContextCandidate   NodeContext = "description_candidate"
	ContextScope       NodeContext = "listing_scope"
	ContextStatus      NodeContext = "status"
)

// Refs identify only one captured epoch. Recipes never retain these refs.
type Node struct {
	Ref        string            `json:"ref"`
	ParentRef  string            `json:"parent_ref,omitempty"`
	Tag        string            `json:"tag"`
	Role       string            `json:"role,omitempty"`
	Text       string            `json:"text,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Context    NodeContext       `json:"context"`
	OwnerJobID string            `json:"owner_job_id"`
	Owned      bool              `json:"owned"`
	Visible    bool              `json:"visible"`
	Bounds     Rect              `json:"bounds"`
	TextLength int               `json:"text_length"`
	Truncated  bool              `json:"truncated"`
	Collapsed  bool              `json:"collapsed"`
	CanExpand  bool              `json:"can_expand"`
	CanScroll  bool              `json:"can_scroll"`
}

type Snapshot struct {
	Version                int       `json:"version"`
	ID                     string    `json:"snapshot_id"`
	Scope                  Scope     `json:"scope"`
	Target                 Target    `json:"target"`
	Identity               Identity  `json:"identity"`
	Nodes                  []Node    `json:"nodes"`
	Candidates             []string  `json:"description_candidates"`
	AllowedActions         []Action  `json:"allowed_actions"`
	LayoutSignature        string    `json:"layout_signature"`
	Loading                bool      `json:"loading"`
	AuthenticationRequired bool      `json:"authentication_required"`
	Expired                bool      `json:"expired"`
	Truncated              bool      `json:"truncated"`
	CapturedAt             time.Time `json:"captured_at"`
	// Captures are owned runtime data, never reusable recipe fields.
	Screenshot []byte `json:"-"`
}

type ActionKind string

const (
	ActionWait   ActionKind = "wait"
	ActionScroll ActionKind = "scroll"
	ActionExpand ActionKind = "expand"
)

type Action struct {
	Kind   ActionKind `json:"kind"`
	Ref    string     `json:"ref,omitempty"`
	Amount int        `json:"amount,omitempty"`
}

// A model can select observed nodes and already-permitted reading actions.
// It cannot supply description text, selectors, code or application actions.
type Plan struct {
	Version         int      `json:"version"`
	SnapshotID      string   `json:"snapshot_id"`
	TitleRef        string   `json:"title_ref,omitempty"`
	CompanyRef      string   `json:"company_ref,omitempty"`
	DescriptionRefs []string `json:"description_refs"`
	Actions         []Action `json:"actions,omitempty"`
}

type Verdict struct {
	Ref           string `json:"ref"`
	IsDescription bool   `json:"is_description"`
	Supported     bool   `json:"supported"`
	Complete      bool   `json:"complete"`
}
type Verification struct {
	SnapshotID string    `json:"snapshot_id"`
	Verdicts   []Verdict `json:"verdicts"`
}

type Source string

const (
	SourceDeterministic Source = "deterministic"
	SourceRecovery      Source = "recovery"
	SourceRecipe        Source = "verified_recipe"
	SourceModel         Source = "verified_model"
)

type Metrics struct {
	DeterministicReads   int   `json:"deterministic_reads"`
	RecipeHits           int   `json:"recipe_hits"`
	RecoveryActions      int   `json:"recovery_actions"`
	ProposalRequests     int   `json:"proposal_requests"`
	VerificationRequests int   `json:"verification_requests"`
	HTTPAttempts         int   `json:"http_attempts"`
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CostMicroUSD         int64 `json:"cost_micro_usd"`
	CostKnown            bool  `json:"cost_known"`
	BrowserMS            int64 `json:"browser_ms"`
	LatencyMS            int64 `json:"latency_ms"`
}

type Result struct {
	Target          Target   `json:"target"`
	Identity        Identity `json:"identity"`
	Description     string   `json:"description"`
	PostedDate      string   `json:"posted_date,omitempty"`
	DueDate         string   `json:"due_date,omitempty"`
	Source          Source   `json:"source"`
	RecipeID        string   `json:"recipe_id,omitempty"`
	RecipeVersion   int      `json:"recipe_version,omitempty"`
	LayoutSignature string   `json:"layout_signature"`
	Metrics         Metrics  `json:"metrics"`
}

type Limits struct {
	MaxNodes            int   `json:"max_nodes"`
	MaxTextChars        int   `json:"max_text_chars"`
	MaxImageBytes       int   `json:"max_image_bytes"`
	MaxDescriptionChars int   `json:"max_description_chars"`
	MaxRequests         int   `json:"max_requests"`
	MaxTokens           int64 `json:"max_tokens"`
	MaxCostMicroUSD     int64 `json:"max_cost_micro_usd"`
	MaxRecoveryActions  int   `json:"max_recovery_actions"`
	WallClockSeconds    int   `json:"wall_clock_seconds"`
}

func DefaultLimits() Limits {
	return Limits{MaxNodes: 160, MaxTextChars: 12000, MaxImageBytes: 256 * 1024, MaxDescriptionChars: 65536, MaxRequests: 4, MaxTokens: 24000, MaxCostMicroUSD: 250000, MaxRecoveryActions: 4, WallClockSeconds: 60}
}

type Mode string

const (
	ModeOff   Mode = "off"
	ModePilot Mode = "pilot"
	ModeOn    Mode = "on"
)

type Config struct {
	Recovery        Mode     `json:"recovery"`
	Reuse           Mode     `json:"reuse"`
	PilotUsers      []string `json:"pilot_users,omitempty"`
	Limits          Limits   `json:"limits"`
	PrivateCaptures bool     `json:"private_captures"`
	Revision        string   `json:"revision,omitempty"`
}

// Page operations are implemented by trusted platform/browser code. Read
// reacquires and checks current identity/refs; Act checks the current allowance.
type Page interface {
	Known(context.Context, Target, Limits) (Result, error)
	Capture(context.Context, Target, Limits) (Snapshot, error)
	Read(context.Context, Target, Snapshot, Plan, Limits) (Result, error)
	Act(context.Context, Target, Snapshot, Action) error
}
type Model interface {
	Propose(context.Context, Snapshot) (Plan, error)
	Verify(context.Context, Snapshot, Plan) (Verification, error)
}

type FailureCode string

const (
	FailureIdentity     FailureCode = "identity_unverified"
	FailureDrift        FailureCode = "identity_drift"
	FailureOrigin       FailureCode = "unsupported_origin"
	FailureAuth         FailureCode = "authentication_required"
	FailureExpired      FailureCode = "listing_expired"
	FailureMissing      FailureCode = "description_missing"
	FailureThin         FailureCode = "description_incomplete"
	FailureAmbiguous    FailureCode = "description_ambiguous"
	FailureStale        FailureCode = "stale_snapshot"
	FailureBrowser      FailureCode = "browser_failed"
	FailurePlan         FailureCode = "invalid_plan"
	FailureVerification FailureCode = "verification_failed"
	FailureBudget       FailureCode = "reading_budget_exceeded"
	FailurePersistence  FailureCode = "reading_persistence_failed"
	FailureProvider     FailureCode = "reading_provider_failed"
	FailureQuota        FailureCode = "reading_quota_exceeded"
	FailureCanceled     FailureCode = "reading_canceled"
	FailureDeadline     FailureCode = "reading_deadline_exceeded"
)

type ReadError struct {
	Code  FailureCode
	Cause error
}

func (e *ReadError) Error() string {
	if e.Cause != nil {
		return "job page reading: " + string(e.Code) + ": " + e.Cause.Error()
	}
	return "job page reading: " + string(e.Code)
}
func (e *ReadError) Unwrap() error { return e.Cause }
func ErrorCode(err error) FailureCode {
	if errors.Is(err, context.Canceled) {
		return FailureCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return FailureDeadline
	}
	if errors.Is(err, quota.ErrExceeded) {
		return FailureQuota
	}
	if errors.Is(err, llm.ErrBillingPersistFailed) {
		return FailurePersistence
	}
	var e *ReadError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
