package jobpage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNoSafeRecipe refuses to generalize an otherwise proven live read.
// It is raised only after full identity/content verification succeeds.
var ErrNoSafeRecipe = errors.New("verified page has no unambiguous reusable structure")

type RecipeState string

const (
	RecipeActive      RecipeState = "active"
	RecipeInvalidated RecipeState = "invalidated"
	RecipeRetired     RecipeState = "retired"
)

// StructuralSelector contains only reusable, observed structure. Subject
// attributes are resolved from the current target, never the learned vacancy.
type StructuralSelector struct {
	Tag               string            `json:"tag"`
	Role              string            `json:"role,omitempty"`
	Context           NodeContext       `json:"context"`
	ParentTag         string            `json:"parent_tag,omitempty"`
	ParentRole        string            `json:"parent_role,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	SubjectAttributes map[string]string `json:"subject_attributes,omitempty"`
}

type Recipe struct {
	ID                   string               `json:"id"`
	Version              int                  `json:"version"`
	Scope                Scope                `json:"scope"`
	LayoutSignature      string               `json:"layout_signature"`
	DescriptionSelectors []StructuralSelector `json:"description_selectors"`
	VerificationKind     string               `json:"verification_kind"`
	VerifiedAt           time.Time            `json:"verified_at"`
	State                RecipeState          `json:"state"`
	StateReason          string               `json:"state_reason,omitempty"`
	UpdatedAt            time.Time            `json:"updated_at"`
	seal                 [32]byte
}

type RecipeStore interface {
	Lookup(context.Context, Scope, string) ([]Recipe, error)
	SaveVerified(context.Context, Recipe) (Recipe, error)
	Invalidate(context.Context, string, string) error
}

func recipeDefinition(r Recipe) ([]byte, error) {
	return json.Marshal(struct {
		Version              int
		Scope                Scope
		LayoutSignature      string
		DescriptionSelectors []StructuralSelector
	}{r.Version, r.Scope, r.LayoutSignature, r.DescriptionSelectors})
}

func recipeID(r Recipe) string {
	raw, _ := recipeDefinition(r)
	return fmt.Sprintf("jpr1:%x", sha256.Sum256(raw))
}

func recipeSeal(r Recipe) [32]byte {
	raw, _ := json.Marshal(r)
	return sha256.Sum256(append([]byte("verified-live-recipe-v1\n"), raw...))
}

var selectorToken = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_ -]{0,95}$`)
var signaturePattern = regexp.MustCompile(`^jp1:[a-f0-9]{64}$`)

func safeSelectorTag(tag string) bool {
	switch tag {
	case "article", "section", "main", "div", "p", "span", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6", "header", "footer", "aside", "body":
		return true
	}
	return false
}

func safeSelectorRole(role string) bool {
	switch role {
	case "", "article", "region", "document", "main", "generic", "paragraph", "list", "listitem", "group", "heading", "presentation", "none":
		return true
	}
	return false
}

func safeRecipeAttribute(k, v string) bool {
	if !selectorToken.MatchString(v) || volatileValue.MatchString(v) {
		return false
	}
	switch k {
	case "data-testid", "data-automation", "data-test", "data-sdui-screen":
		return true
	case "role":
		return safeSelectorRole(v)
	case "itemprop":
		return v == "description"
	case "aria-label":
		switch normalizeText(v) {
		case "job description", "job details", "about the job", "position description", "description":
			return true
		}
	}
	return false
}

func validateRecipe(r Recipe) error {
	_, scope, err := NormalizeTarget(Target{Platform: r.Scope.Platform, URL: r.Scope.Origin + map[string]string{"linkedin": "/jobs/view/1/", "seek": "/job/1"}[r.Scope.Platform]})
	if err != nil || scope != r.Scope || r.Version != RecipeVersion || !signaturePattern.MatchString(r.LayoutSignature) || len(r.DescriptionSelectors) == 0 || len(r.DescriptionSelectors) > 16 || r.ID != recipeID(r) {
		return &ReadError{Code: FailurePlan}
	}
	if r.State != RecipeActive && r.State != RecipeInvalidated && r.State != RecipeRetired {
		return &ReadError{Code: FailurePlan}
	}
	if r.VerificationKind != "independent_semantic_live" || r.VerifiedAt.IsZero() {
		return &ReadError{Code: FailureVerification}
	}
	for _, q := range r.DescriptionSelectors {
		if !safeSelectorTag(q.Tag) || (q.ParentTag != "" && !safeSelectorTag(q.ParentTag)) || !safeSelectorRole(q.Role) || !safeSelectorRole(q.ParentRole) || (q.Context != ContextDescription && q.Context != ContextCandidate) || len(q.Attributes) > 10 || len(q.SubjectAttributes) > 2 {
			return &ReadError{Code: FailurePlan}
		}
		for k, v := range q.Attributes {
			if !safeRecipeAttribute(k, v) {
				return &ReadError{Code: FailurePlan}
			}
		}
		for k, v := range q.SubjectAttributes {
			if !((k == "data-job-id" && v == "job_id") || (k == "data-entity-urn" && r.Scope.Platform == "linkedin" && v == "linkedin_job_urn")) {
				return &ReadError{Code: FailurePlan}
			}
		}
	}
	return nil
}

func selectorMatches(s Snapshot, q StructuralSelector, n Node) bool {
	if !n.Owned || n.OwnerJobID != s.Target.JobID || n.Tag != q.Tag || n.Role != q.Role || n.Context != q.Context {
		return false
	}
	if q.ParentTag != "" {
		parent, ok := s.Node(n.ParentRef)
		if !ok || parent.Tag != q.ParentTag || parent.Role != q.ParentRole {
			return false
		}
	}
	for k, v := range q.Attributes {
		if n.Attributes[k] != v {
			return false
		}
	}
	for k, v := range q.SubjectAttributes {
		wanted := s.Target.JobID
		if v == "linkedin_job_urn" {
			wanted = "urn:li:jobPosting:" + wanted
		}
		if n.Attributes[k] != wanted {
			return false
		}
	}
	return true
}

// PlanFromRecipe binds reusable structure to fresh refs. Ambiguity is a hard
// failure; neither DOM order nor cached text can break a tie.
func PlanFromRecipe(s Snapshot, r Recipe, limits Limits) (Plan, error) {
	p := Plan{Version: SnapshotVersion, SnapshotID: s.ID}
	if err := ValidateSnapshot(s, limits); err != nil {
		return p, err
	}
	if err := validateRecipe(r); err != nil {
		return p, err
	}
	if r.State != RecipeActive || r.Scope != s.Scope || r.LayoutSignature != s.LayoutSignature || s.LayoutSignature != StructuralSignature(s) {
		return p, &ReadError{Code: FailureStale}
	}
	if s.Loading || s.Truncated {
		return p, &ReadError{Code: FailureThin}
	}
	for _, q := range r.DescriptionSelectors {
		var matches []Node
		for _, n := range s.Nodes {
			if selectorMatches(s, q, n) {
				matches = append(matches, n)
			}
		}
		if len(matches) != 1 {
			return p, &ReadError{Code: FailureAmbiguous}
		}
		n := matches[0]
		if !n.Visible || n.Truncated || n.Collapsed {
			return p, &ReadError{Code: FailureThin}
		}
		p.DescriptionRefs = append(p.DescriptionRefs, n.Ref)
	}
	return p, ValidatePlan(s, p, limits)
}

// BuildVerifiedRecipe requires independent verification and an identity-checked
// live extraction that exactly agrees with the complete selected observation.
// Its private seal prevents callers promoting forged or subsequently edited data.
func BuildVerifiedRecipe(ctx context.Context, s Snapshot, p Plan, live Result, v Verification, limits Limits) (Recipe, error) {
	if err := ctx.Err(); err != nil {
		return Recipe{}, err
	}
	if err := ValidatePlan(s, p, limits); err != nil {
		return Recipe{}, err
	}
	if err := ValidateVerification(s, p, v); err != nil {
		return Recipe{}, err
	}
	if s.Loading || s.Truncated || len(p.Actions) > 0 || s.LayoutSignature != StructuralSignature(s) {
		return Recipe{}, &ReadError{Code: FailureThin}
	}
	current, scope, err := NormalizeTarget(live.Target)
	if err != nil || scope != s.Scope || current.JobID != s.Target.JobID || !live.Identity.Verified || live.Identity.JobID != s.Target.JobID || normalizeText(live.Identity.Title) != normalizeText(s.Identity.Title) || normalizeText(live.Identity.Company) != normalizeText(s.Identity.Company) || live.LayoutSignature != s.LayoutSignature || live.Description == "" || utf8.RuneCountInString(live.Description) > limits.MaxDescriptionChars {
		return Recipe{}, &ReadError{Code: FailureVerification}
	}
	r := Recipe{Version: RecipeVersion, Scope: s.Scope, LayoutSignature: s.LayoutSignature, VerificationKind: "independent_semantic_live", VerifiedAt: time.Now().UTC(), State: RecipeActive}
	var texts []string
	for _, ref := range p.DescriptionRefs {
		n, _ := s.Node(ref)
		if !n.Visible || n.Collapsed || n.Truncated || n.Text == "" || (n.TextLength > 0 && n.TextLength != utf8.RuneCountInString(n.Text)) {
			return Recipe{}, &ReadError{Code: FailureThin}
		}
		q := StructuralSelector{Tag: n.Tag, Role: n.Role, Context: n.Context, Attributes: map[string]string{}, SubjectAttributes: map[string]string{}}
		if parent, ok := s.Node(n.ParentRef); ok {
			q.ParentTag, q.ParentRole = parent.Tag, parent.Role
		}
		for k, value := range n.Attributes {
			switch {
			case k == "data-job-id" && value == s.Target.JobID:
				q.SubjectAttributes[k] = "job_id"
			case k == "data-entity-urn" && s.Scope.Platform == "linkedin" && value == "urn:li:jobPosting:"+s.Target.JobID:
				q.SubjectAttributes[k] = "linkedin_job_urn"
			case safeRecipeAttribute(k, value) && stableAttribute(k, value, s.Target):
				private := false
				for _, attribution := range []string{s.Target.UserID, s.Target.RunID, s.Target.AttemptID, s.Target.RecordID} {
					if attribution != "" && strings.Contains(normalizeText(value), normalizeText(attribution)) {
						private = true
					}
				}
				if !private {
					q.Attributes[k] = value
				}
			}
		}
		r.DescriptionSelectors = append(r.DescriptionSelectors, q)
		texts = append(texts, n.Text)
	}
	if normalizeText(strings.Join(texts, "\n\n")) != normalizeText(live.Description) {
		return Recipe{}, &ReadError{Code: FailureVerification}
	}
	r.ID = recipeID(r)
	if _, err := PlanFromRecipe(s, r, limits); err != nil {
		if ErrorCode(err) == FailureAmbiguous {
			return Recipe{}, fmt.Errorf("%w: %v", ErrNoSafeRecipe, err)
		}
		return Recipe{}, err
	}
	if err := ctx.Err(); err != nil {
		return Recipe{}, err
	}
	r.seal = recipeSeal(r)
	return r, nil
}
