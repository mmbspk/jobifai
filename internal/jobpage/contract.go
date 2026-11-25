package jobpage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var refPattern = regexp.MustCompile(`^n[0-9]{1,4}$`)
var listingPath = regexp.MustCompile(`^/jobs/view/([0-9]+)(?:/|$)`)
var seekPath = regexp.MustCompile(`^/job/([0-9]+)(?:/|$)`)

func NormalizeTarget(t Target) (Target, Scope, error) {
	u, err := url.Parse(t.URL)
	if err != nil || u.User != nil || u.Scheme != "https" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return t, Scope{}, &ReadError{Code: FailureOrigin}
	}
	host := strings.ToLower(u.Hostname())
	var platform, id string
	switch host {
	case "linkedin.com", "www.linkedin.com":
		platform = "linkedin"
		host = "www.linkedin.com"
		if match := listingPath.FindStringSubmatch(u.Path); len(match) > 1 {
			id = match[1]
		} else if strings.HasPrefix(u.Path, "/jobs/search") {
			id = u.Query().Get("currentJobId")
		}
	case "au.seek.com", "seek.com.au", "www.seek.com.au", "nz.seek.com", "seek.co.nz", "www.seek.co.nz":
		platform = "seek"
		if host == "au.seek.com" || host == "seek.com.au" || host == "www.seek.com.au" {
			host = "www.seek.com.au"
		} else {
			host = "www.seek.co.nz"
		}
		if match := seekPath.FindStringSubmatch(u.Path); len(match) > 1 {
			id = match[1]
		}
	default:
		return t, Scope{}, &ReadError{Code: FailureOrigin}
	}
	if id == "" || !regexp.MustCompile(`^[0-9]+$`).MatchString(id) || (t.JobID != "" && t.JobID != id) || (t.Platform != "" && t.Platform != platform) {
		return t, Scope{}, &ReadError{Code: FailureIdentity}
	}
	t.Platform, t.JobID = platform, id
	t.Title, t.Company = strings.TrimSpace(t.Title), strings.TrimSpace(t.Company)
	// Model input and diagnostics use a canonical listing URL, not tracking,
	// session, fragment or credential parameters supplied in the original URL.
	if platform == "linkedin" {
		t.URL = "https://" + host + "/jobs/view/" + id + "/"
	} else {
		t.URL = "https://" + host + "/job/" + id
	}
	return t, Scope{Platform: platform, PageType: "job_listing", Origin: "https://" + host, SnapshotVersion: SnapshotVersion}, nil
}

func normalizeText(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func (s Snapshot) Node(ref string) (Node, bool) {
	for _, n := range s.Nodes {
		if n.Ref == ref {
			return n, true
		}
	}
	return Node{}, false
}

func ValidateSnapshot(s Snapshot, limits Limits) error {
	t, scope, err := NormalizeTarget(s.Target)
	if err != nil {
		return err
	}
	if s.AuthenticationRequired {
		return &ReadError{Code: FailureAuth}
	}
	if s.Expired {
		return &ReadError{Code: FailureExpired}
	}
	if s.Version != SnapshotVersion || s.Scope != scope || s.ID == "" || s.LayoutSignature == "" {
		return &ReadError{Code: FailureStale}
	}
	if !s.Identity.Verified || s.Identity.JobID != t.JobID || len(s.Identity.HeaderRefs) == 0 || (t.Title != "" && normalizeText(t.Title) != normalizeText(s.Identity.Title)) || (t.Company != "" && normalizeText(t.Company) != normalizeText(s.Identity.Company)) {
		return &ReadError{Code: FailureIdentity}
	}
	subject, header := false, false
	for _, e := range s.Identity.Evidence {
		switch e {
		case "subject_attribute", "job_posting_identifier":
			subject = true
		case "primary_header":
			header = true
		}
	}
	if !subject || !header {
		return &ReadError{Code: FailureIdentity}
	}
	if limits.MaxNodes <= 0 || limits.MaxTextChars <= 0 || len(s.Nodes) > limits.MaxNodes || len(s.Screenshot) > limits.MaxImageBytes {
		return &ReadError{Code: FailureBudget}
	}
	seen := map[string]bool{}
	total := 0
	for _, n := range s.Nodes {
		if !refPattern.MatchString(n.Ref) || seen[n.Ref] || len(n.Attributes) > 12 || len(n.Tag) > 32 || len(n.Role) > 80 || !n.Owned || n.OwnerJobID != t.JobID {
			return &ReadError{Code: FailureIdentity}
		}
		seen[n.Ref] = true
		total += utf8.RuneCountInString(n.Text)
		if total > limits.MaxTextChars {
			return &ReadError{Code: FailureBudget}
		}
		for key, value := range n.Attributes {
			if !allowedSnapshotAttribute(key) || len(value) > 512 {
				return &ReadError{Code: FailureIdentity}
			}
		}
		switch n.Context {
		case ContextHeader, ContextDescription, ContextCandidate, ContextScope, ContextStatus:
		default:
			return &ReadError{Code: FailureIdentity}
		}
	}
	for _, ref := range s.Identity.HeaderRefs {
		n, ok := s.Node(ref)
		if !ok || n.Context != ContextHeader {
			return &ReadError{Code: FailureIdentity}
		}
	}
	for _, n := range s.Nodes {
		if n.ParentRef != "" && !seen[n.ParentRef] {
			return &ReadError{Code: FailureStale}
		}
	}
	return nil
}

func allowedSnapshotAttribute(k string) bool {
	switch k {
	case "data-job-id", "data-entity-urn", "data-testid", "data-automation", "data-test", "data-sdui-screen", "aria-label", "itemprop", "role":
		return true
	}
	return false
}

func ValidatePlan(s Snapshot, p Plan, limits Limits) error {
	if err := ValidateSnapshot(s, limits); err != nil {
		return err
	}
	if p.Version != SnapshotVersion || p.SnapshotID != s.ID {
		return &ReadError{Code: FailureStale}
	}
	if len(p.DescriptionRefs) == 0 || len(p.DescriptionRefs) > 16 || len(p.Actions) > limits.MaxRecoveryActions {
		return &ReadError{Code: FailurePlan}
	}
	seen := map[string]bool{}
	for _, ref := range p.DescriptionRefs {
		n, ok := s.Node(ref)
		if !ok || seen[ref] || (n.Context != ContextDescription && n.Context != ContextCandidate) {
			return &ReadError{Code: FailurePlan}
		}
		seen[ref] = true
	}
	// Selecting both a container and its descendant would duplicate content and
	// can hide conflicting ownership. Require disjoint observed regions.
	for _, ref := range p.DescriptionRefs {
		n, _ := s.Node(ref)
		for depth := 0; n.ParentRef != ""; depth++ {
			if depth > len(s.Nodes) || seen[n.ParentRef] {
				return &ReadError{Code: FailurePlan}
			}
			var ok bool
			n, ok = s.Node(n.ParentRef)
			if !ok {
				return &ReadError{Code: FailureStale}
			}
		}
	}
	for _, ref := range []string{p.TitleRef, p.CompanyRef} {
		if ref != "" {
			n, ok := s.Node(ref)
			if !ok || n.Context != ContextHeader {
				return &ReadError{Code: FailurePlan}
			}
		}
	}
	for _, a := range p.Actions {
		if err := ValidateAction(s, a); err != nil {
			return err
		}
	}
	return nil
}

func ValidateAction(s Snapshot, a Action) error {
	if a.Kind == ActionWait {
		if a.Ref != "" || a.Amount < 0 || a.Amount > 2000 {
			return &ReadError{Code: FailurePlan}
		}
		return nil
	}
	n, ok := s.Node(a.Ref)
	if !ok || !n.Owned || n.OwnerJobID != s.Target.JobID || (n.Context != ContextDescription && n.Context != ContextCandidate && n.Context != ContextScope) {
		return &ReadError{Code: FailurePlan}
	}
	switch a.Kind {
	case ActionScroll:
		if !n.CanScroll || a.Amount < -1000 || a.Amount > 1000 {
			return &ReadError{Code: FailurePlan}
		}
	case ActionExpand:
		if !n.CanExpand || a.Amount != 0 {
			return &ReadError{Code: FailurePlan}
		}
	default:
		return &ReadError{Code: FailurePlan}
	}
	for _, allowed := range s.AllowedActions {
		if allowed.Kind == a.Kind && allowed.Ref == a.Ref {
			return nil
		}
	}
	return &ReadError{Code: FailurePlan}
}

func DecodePlan(raw string) (Plan, error) {
	var p Plan
	if err := decodeStrict(raw, &p); err != nil {
		return p, err
	}
	return p, nil
}
func DecodeVerification(raw string) (Verification, error) {
	var v Verification
	err := decodeStrict(raw, &v)
	return v, err
}
func decodeStrict(raw string, dst any) error {
	if len(raw) > 16*1024 {
		return &ReadError{Code: FailurePlan}
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &ReadError{Code: FailurePlan, Cause: fmt.Errorf("typed response could not be decoded")}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return &ReadError{Code: FailurePlan}
	}
	return nil
}
func ValidateVerification(s Snapshot, p Plan, v Verification) error {
	if v.SnapshotID != s.ID || len(v.Verdicts) != len(p.DescriptionRefs) {
		return &ReadError{Code: FailureVerification}
	}
	wanted := map[string]bool{}
	for _, ref := range p.DescriptionRefs {
		wanted[ref] = true
	}
	for _, e := range v.Verdicts {
		if !wanted[e.Ref] || !e.IsDescription || !e.Supported || !e.Complete {
			return &ReadError{Code: FailureVerification}
		}
		delete(wanted, e.Ref)
	}
	if len(wanted) != 0 {
		return &ReadError{Code: FailureVerification}
	}
	return nil
}

// Signature ignores vacancy text, refs, IDs, generated classes, layout pixels
// and repeated paragraph/list-item counts. Replay still verifies live content.
func StructuralSignature(s Snapshot) string {
	shapes := map[string]bool{}
	for _, n := range s.Nodes {
		var attrs []string
		for k, v := range n.Attributes {
			if stableAttribute(k, v, s.Target) {
				attrs = append(attrs, k+"="+v)
			} else if k == "data-job-id" || k == "data-entity-urn" {
				attrs = append(attrs, k+"=$job")
			}
		}
		sort.Strings(attrs)
		parentTag := ""
		if parent, ok := s.Node(n.ParentRef); ok {
			parentTag = parent.Tag + "/" + parent.Role
		}
		shapes[strings.Join([]string{string(n.Context), parentTag, n.Tag, n.Role, strings.Join(attrs, ";")}, "|")] = true
	}
	var rows []string
	for shape := range shapes {
		rows = append(rows, shape)
	}
	sort.Strings(rows)
	raw, _ := json.Marshal(struct {
		Scope  Scope
		Shapes []string
	}{s.Scope, rows})
	return fmt.Sprintf("jp1:%x", sha256.Sum256(raw))
}

var volatileValue = regexp.MustCompile(`(?i)([0-9]{5,}|[a-f0-9]{8,}|https?://|@)`)

func stableAttribute(k, v string, t Target) bool {
	if k == "data-job-id" || k == "data-entity-urn" || !allowedSnapshotAttribute(k) || v == "" || volatileValue.MatchString(v) {
		return false
	}
	lower := normalizeText(v)
	for _, current := range []string{t.JobID, t.Title, t.Company} {
		if current != "" && strings.Contains(lower, normalizeText(current)) {
			return false
		}
	}
	return true
}
