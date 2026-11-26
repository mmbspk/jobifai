package jobpage

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

//go:embed capture.js
var captureScript string

// Baseline callbacks are trusted platform adapters, never generated browser
// code. Capture still independently requires an owned subject and live header.
type Baseline struct {
	Known  func(context.Context, Target, Limits) (Result, error)
	Verify func(context.Context, Target) (Identity, error)
}

type BrowserPage struct {
	page     *rod.Page
	baseline Baseline
	mu       sync.Mutex
	epochs   map[string]browserEpoch
}
type browserEpoch struct {
	material string
	limits   Limits
}
type browserCapture struct {
	Identity               Identity    `json:"identity"`
	Nodes                  []Node      `json:"nodes"`
	Candidates             []string    `json:"candidates"`
	Actions                []Action    `json:"allowed_actions"`
	Loading                bool        `json:"loading"`
	AuthenticationRequired bool        `json:"authentication_required"`
	Expired                bool        `json:"expired"`
	Truncated              bool        `json:"truncated"`
	Epoch                  string      `json:"epoch_material"`
	PrivacyBoxes           []Rect      `json:"privacy_boxes"`
	Failure                FailureCode `json:"failure"`
	Description            string      `json:"description"`
	PostedDate             string      `json:"posted_date"`
	DueDate                string      `json:"due_date"`
}

func NewBrowserPage(page *rod.Page, baseline Baseline) *BrowserPage {
	return &BrowserPage{page: page, baseline: baseline, epochs: make(map[string]browserEpoch)}
}

func (p *BrowserPage) checkTarget(ctx context.Context, target Target) (Target, Scope, error) {
	if err := ctx.Err(); err != nil {
		return target, Scope{}, err
	}
	target, scope, err := NormalizeTarget(target)
	if err != nil {
		return target, scope, err
	}
	if p.page == nil {
		return target, scope, &ReadError{Code: FailureBrowser}
	}
	info, err := p.page.Context(ctx).Info()
	if err != nil {
		return target, scope, browserError(ctx, err)
	}
	if authenticationURL(info.URL, scope) {
		return target, scope, &ReadError{Code: FailureAuth}
	}
	current, currentScope, err := NormalizeTarget(Target{URL: info.URL})
	if err != nil {
		return target, scope, &ReadError{Code: FailureDrift, Cause: err}
	}
	if current.JobID != target.JobID || currentScope != scope {
		return target, scope, &ReadError{Code: FailureDrift}
	}
	return target, scope, nil
}

func authenticationURL(raw string, scope Scope) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	if scope.Platform == "seek" && strings.EqualFold(u.Hostname(), "login.seek.com") {
		return true
	}
	path := strings.ToLower(u.Path)
	auth := false
	for _, prefix := range []string{"/login", "/uas/login", "/checkpoint", "/authwall", "/signin", "/sign-in", "/oauth/login", "/oauth/authorize"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			auth = true
			break
		}
	}
	if !auth {
		return false
	}
	probe := *u
	probe.Path = "/jobs/view/0/"
	if scope.Platform == "seek" {
		probe.Path = "/job/0"
	}
	probe.RawPath, probe.RawQuery, probe.Fragment = "", "", ""
	_, origin, err := NormalizeTarget(Target{URL: probe.String()})
	return err == nil && origin == scope
}

func browserError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &ReadError{Code: FailureBrowser, Cause: err}
}

func (p *BrowserPage) options(ctx context.Context, target Target, limits Limits) (map[string]any, error) {
	opts := map[string]any{"job_id": target.JobID, "title": target.Title, "company": target.Company, "max_nodes": limits.MaxNodes, "max_text_chars": limits.MaxTextChars}
	if p.baseline.Verify != nil {
		identity, err := p.baseline.Verify(ctx, target)
		if err != nil {
			return nil, err
		}
		if !identity.Verified || identity.JobID != target.JobID || (target.Title != "" && normalizeText(target.Title) != normalizeText(identity.Title)) || (target.Company != "" && normalizeText(target.Company) != normalizeText(identity.Company)) {
			return nil, &ReadError{Code: FailureIdentity}
		}
		opts["trusted_verified"], opts["trusted_title"], opts["trusted_company"] = true, identity.Title, identity.Company
	}
	return opts, nil
}

func (p *BrowserPage) eval(ctx context.Context, opts map[string]any) (browserCapture, error) {
	var raw browserCapture
	value, err := p.page.Context(ctx).Eval(captureScript, opts)
	if err != nil {
		return raw, browserError(ctx, err)
	}
	if err := value.Value.Unmarshal(&raw); err != nil {
		return raw, browserError(ctx, err)
	}
	if raw.Failure != "" {
		return raw, &ReadError{Code: raw.Failure}
	}
	return raw, nil
}

func (p *BrowserPage) capture(ctx context.Context, target Target, limits Limits) (Snapshot, browserCapture, error) {
	target, scope, err := p.checkTarget(ctx, target)
	if err != nil {
		return Snapshot{}, browserCapture{}, err
	}
	if limits.MaxNodes <= 0 || limits.MaxTextChars <= 0 {
		return Snapshot{}, browserCapture{}, &ReadError{Code: FailureBudget}
	}
	opts, err := p.options(ctx, target, limits)
	if err != nil {
		return Snapshot{}, browserCapture{}, err
	}
	raw, err := p.eval(ctx, opts)
	if err != nil {
		return Snapshot{}, raw, err
	}
	if _, _, err := p.checkTarget(ctx, target); err != nil {
		return Snapshot{}, raw, err
	}
	s := Snapshot{Version: SnapshotVersion, ID: fmt.Sprintf("%x", sha256.Sum256([]byte(raw.Epoch))), Scope: scope, Target: target, Identity: raw.Identity, Nodes: raw.Nodes, Candidates: raw.Candidates, AllowedActions: raw.Actions, Loading: raw.Loading, AuthenticationRequired: raw.AuthenticationRequired, Expired: raw.Expired, Truncated: raw.Truncated, CapturedAt: time.Now().UTC()}
	s.LayoutSignature = StructuralSignature(s)
	if err := ValidateSnapshot(s, limits); err != nil {
		return s, raw, err
	}
	// Retain only bounded current runtime epochs; recipes use structural fields.
	if len(p.epochs) >= 32 {
		p.epochs = make(map[string]browserEpoch)
	}
	p.epochs[s.ID] = browserEpoch{material: raw.Epoch, limits: limits}
	return s, raw, nil
}

func (p *BrowserPage) Capture(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, _, err := p.capture(ctx, target, limits)
	return s, err
}

func validDescription(text string, limits Limits) error {
	if utf8.RuneCountInString(strings.TrimSpace(text)) < 100 {
		return &ReadError{Code: FailureThin}
	}
	if limits.MaxDescriptionChars <= 0 || utf8.RuneCountInString(text) > limits.MaxDescriptionChars {
		return &ReadError{Code: FailureBudget}
	}
	return nil
}

func (p *BrowserPage) Known(ctx context.Context, target Target, limits Limits) (Result, error) {
	if p.baseline.Known != nil {
		target, _, err := p.checkTarget(ctx, target)
		if err != nil {
			return Result{}, err
		}
		r, err := p.baseline.Known(ctx, target, limits)
		if err != nil {
			return r, err
		}
		if _, _, err := p.checkTarget(ctx, target); err != nil {
			return Result{}, err
		}
		// A legacy reader may retain full text on an explicitly closed listing.
		// Status is a separate live boundary from description extraction.
		status, err := p.page.Context(ctx).Eval(`()=>{const main=document.querySelector('main,[role="main"]');if(!main)return '';return Array.from(main.querySelectorAll('[role="alert"],[data-testid*="status"],[data-automation*="status"]')).filter(n=>!n.closest('aside,nav,form,dialog,[role="dialog"]')&&n.getClientRects().length).map(n=>n.textContent||'').join(' ')}`)
		if err != nil {
			return Result{}, browserError(ctx, err)
		}
		closed := strings.ToLower(status.Value.Str())
		for _, marker := range []string{"job has expired", "job expired", "no longer available", "no longer accepting applications", "job is closed", "position has been filled"} {
			if strings.Contains(closed, marker) {
				return Result{}, &ReadError{Code: FailureExpired}
			}
		}
		if !r.Identity.Verified || r.Identity.JobID != target.JobID || (target.Title != "" && normalizeText(target.Title) != normalizeText(r.Identity.Title)) || (target.Company != "" && normalizeText(target.Company) != normalizeText(r.Identity.Company)) {
			return Result{}, &ReadError{Code: FailureIdentity}
		}
		if err := validDescription(r.Description, limits); err != nil {
			return Result{}, err
		}
		r.Target = target
		r.Source = SourceDeterministic
		return r, nil
	}
	s, err := p.Capture(ctx, target, limits)
	for attempt := 0; ErrorCode(err) == FailureIdentity && attempt < limits.MaxRecoveryActions; attempt++ {
		// Waiting for an observed loading placeholder does not borrow listing
		// identity. No actions, interpretation or learning occur before proof.
		loading, e := p.page.Context(ctx).Eval(`()=>Array.from(document.querySelectorAll('main [aria-busy="true"],main [role="progressbar"],main [data-testid*="skeleton"],main [data-testid*="loading"]')).some(n=>n.getClientRects().length&&!n.closest('aside,nav,form,dialog,[role="dialog"]'))`)
		if e != nil {
			return Result{}, browserError(ctx, e)
		}
		if !loading.Value.Bool() {
			break
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, ctx.Err()
		case <-timer.C:
		}
		s, err = p.Capture(ctx, target, limits)
	}
	if err != nil {
		return Result{}, err
	}
	plan, err := knownPlan(s)
	if err != nil {
		return Result{}, err
	}
	r, err := p.Read(ctx, target, s, plan, limits)
	r.Source = SourceDeterministic
	return r, err
}

// Known regions are explicit platform description markers. Unfamiliar
// candidate regions require independent semantic verification in the reader.
func knownPlan(s Snapshot) (Plan, error) {
	plan := Plan{Version: SnapshotVersion, SnapshotID: s.ID}
	for _, ref := range s.Candidates {
		n, ok := s.Node(ref)
		if !ok || n.Context != ContextDescription {
			continue
		}
		if n.Truncated || n.Collapsed || !n.Visible {
			return plan, &ReadError{Code: FailureThin}
		}
		plan.DescriptionRefs = append(plan.DescriptionRefs, ref)
	}
	if len(plan.DescriptionRefs) == 0 {
		return plan, &ReadError{Code: FailureMissing}
	}
	if len(plan.DescriptionRefs) != 1 {
		return plan, &ReadError{Code: FailureAmbiguous}
	}
	return plan, nil
}

func (p *BrowserPage) Read(ctx context.Context, target Target, s Snapshot, plan Plan, limits Limits) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ValidateSnapshot(s, limits); err != nil {
		return Result{}, err
	}
	if err := ValidatePlan(s, plan, limits); err != nil {
		return Result{}, err
	}
	epoch, ok := p.epochs[s.ID]
	if !ok {
		return Result{}, &ReadError{Code: FailureStale}
	}
	fresh, _, err := p.capture(ctx, target, limits)
	if err != nil {
		return Result{}, err
	}
	if fresh.ID != s.ID {
		return Result{}, &ReadError{Code: FailureStale}
	}
	var texts []string
	for _, ref := range plan.DescriptionRefs {
		n, _ := fresh.Node(ref)
		if n.Truncated || n.Collapsed || !n.Visible {
			return Result{}, &ReadError{Code: FailureThin}
		}
		texts = append(texts, n.Text)
	}
	opts, err := p.options(ctx, fresh.Target, limits)
	if err != nil {
		return Result{}, err
	}
	opts["mode"], opts["refs"], opts["expected_epoch"] = "read", plan.DescriptionRefs, epoch.material
	raw, err := p.eval(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	if normalizeText(raw.Description) != normalizeText(strings.Join(texts, "\n\n")) {
		return Result{}, &ReadError{Code: FailureStale}
	}
	if err := validDescription(raw.Description, limits); err != nil {
		return Result{}, err
	}
	if _, _, err := p.checkTarget(ctx, target); err != nil {
		return Result{}, err
	}
	return Result{Target: fresh.Target, Identity: fresh.Identity, Description: raw.Description, PostedDate: raw.PostedDate, DueDate: raw.DueDate, LayoutSignature: fresh.LayoutSignature}, nil
}

func (p *BrowserPage) Act(ctx context.Context, target Target, s Snapshot, action Action) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ValidateAction(s, action); err != nil {
		return err
	}
	epoch, ok := p.epochs[s.ID]
	if !ok {
		return &ReadError{Code: FailureStale}
	}
	fresh, _, err := p.capture(ctx, target, epoch.limits)
	if err != nil {
		return err
	}
	if fresh.ID != s.ID {
		return &ReadError{Code: FailureStale}
	}
	if action.Kind == ActionWait {
		timer := time.NewTimer(time.Duration(action.Amount) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	} else {
		opts, err := p.options(ctx, fresh.Target, epoch.limits)
		if err != nil {
			return err
		}
		opts["mode"], opts["ref"], opts["action"], opts["amount"], opts["expected_epoch"] = "action", action.Ref, action.Kind, action.Amount, epoch.material
		if _, err := p.eval(ctx, opts); err != nil {
			return err
		}
	}
	_, _, err = p.checkTarget(ctx, target)
	return err
}

// ScreenshotFor omits the screenshot when unrelated private overlays touch
// the owned crop. It never returns a whole-page image.
func (p *BrowserPage) ScreenshotFor(ctx context.Context, target Target, s Snapshot, limits Limits) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fresh, raw, err := p.capture(ctx, target, limits)
	if err != nil {
		return nil, err
	}
	if fresh.ID != s.ID {
		return nil, &ReadError{Code: FailureStale}
	}
	var crop Rect
	found := false
	for _, ref := range fresh.Candidates {
		n, _ := fresh.Node(ref)
		if !n.Visible || n.Truncated || n.Bounds.Width <= 0 || n.Bounds.Height <= 0 {
			continue
		}
		if found {
			return nil, nil
		}
		crop = n.Bounds
		found = true
	}
	if !found || crop.Width > 1600 || crop.Height > 1200 || crop.X < 0 || crop.Y < 0 {
		return nil, nil
	}
	for _, box := range raw.PrivacyBoxes {
		if overlaps(crop, box) {
			return nil, nil
		}
	}
	position, err := p.page.Context(ctx).Eval(`()=>({x:scrollX,y:scrollY,width:innerWidth,height:innerHeight})`)
	if err != nil {
		return nil, browserError(ctx, err)
	}
	var scroll struct{ X, Y, Width, Height float64 }
	if err := position.Value.Unmarshal(&scroll); err != nil {
		return nil, browserError(ctx, err)
	}
	if crop.X+crop.Width > scroll.Width || crop.Y+crop.Height > scroll.Height {
		return nil, nil
	}
	shot, err := p.page.Context(ctx).Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng, Clip: &proto.PageViewport{X: crop.X + scroll.X, Y: crop.Y + scroll.Y, Width: crop.Width, Height: crop.Height, Scale: 1}})
	if err != nil {
		return nil, browserError(ctx, err)
	}
	if len(shot) > limits.MaxImageBytes {
		return nil, nil
	}
	// Recheck after capture so an intervening private overlay/rerender omits it.
	after, afterRaw, err := p.capture(ctx, target, limits)
	if err != nil {
		return nil, err
	}
	if after.ID != s.ID {
		return nil, &ReadError{Code: FailureStale}
	}
	for _, box := range afterRaw.PrivacyBoxes {
		if overlaps(crop, box) {
			return nil, nil
		}
	}
	return shot, nil
}
func overlaps(a, b Rect) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}

var _ Page = (*BrowserPage)(nil)
