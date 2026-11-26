package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

// SetJobPageReading is called once during server assembly, after migrations.
func (m *Manager) SetJobPageReading(store *jobpage.SQLStore) {
	m.pageStore = store
	m.pageReader = jobpage.NewReader(store, store.GetConfig, func(ctx context.Context, limits jobpage.Limits) (jobpage.Model, error) {
		userID := llm.CallContextFrom(ctx).UserID
		allow := false
		if m.userTypeChecker != nil {
			admin, tester := m.userTypeChecker(userID)
			allow = admin || tester
		}
		gs := config.ResolveOperationalSettings(m.cfgStore, userID, allow)
		key, personal, err := config.ResolveEffectiveLLMKey(m.cfgStore, m.secrets, userID, allow, &gs)
		if err != nil {
			return nil, err
		}
		base := m.userLLMClient(userID, gs, key, personal)
		if base == nil {
			return nil, errors.New("configured job-page AI client unavailable")
		}
		client, err := m.taskClient(base, gs, domain.TaskJobPageReading)
		if err != nil {
			return nil, err
		}
		return jobpage.NewLLMModel(client, client, m.llmCatalog).NewSession(limits), nil
	})
}

func pageIdentity(r linkedInResolution, t jobpage.Target) jobpage.Identity {
	if r.Title == "" {
		r.Title = t.Title
	}
	return jobpage.Identity{JobID: r.ObservedID, Title: r.Title, Company: r.Company, Verified: r.IdentityVerified, Evidence: []string{"primary_header", "subject_attribute"}}
}

func activateReadMeter(ctx context.Context, meter BrowserUsageMeter, jobID string) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if meter == nil {
		return ctx, nil
	}
	meter.SetJobID(jobID)
	if err := meter.Activate(ctx); err != nil {
		return ctx, err
	}
	return meter.BillingCtx(), nil
}

func preflightReadMeter(ctx context.Context, meter BrowserUsageMeter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if meter == nil {
		return nil
	}
	if preflight, ok := meter.(interface{ BeforeActivation(context.Context) error }); ok {
		return preflight.BeforeActivation(ctx)
	}
	return meter.Err()
}

func (m *Manager) alignReadRunMeter(b *Bot) {
	if m.browserMeterSvc == nil {
		return
	}
	if b.cfg.BrowserMeter != nil {
		b.cfg.BrowserMeter.Stop()
	}
	inc, err := m.browserMeterSvc.BrowserIncrementDuration()
	if err != nil || inc <= 0 {
		inc = 30 * time.Second
	}
	b.cfg.BrowserMeter = NewLiveBrowserMeter(m.browserMeterSvc, b.cfg.UserID, b.cfg.AutomationRunID, inc, nil)
}

func (m *Manager) browserContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *Manager) lockBrowserContext(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.browserMu.TryLock() {
			if err := ctx.Err(); err != nil {
				m.browserMu.Unlock()
				return err
			}
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// The typed read error must survive both the submission and preparation paths.
// The hold remains durable display evidence, while callers retain its cause.
func managerLazyWithReadError(b *Bot, ctx context.Context, req SubmitRequest, page *rod.Page) (*lazyDocGen, error) {
	if b.cfg.JobPageReader == nil {
		return managerLazyForSubmit(b, ctx, req, page), nil
	}
	result, err := b.readJobPage(ctx, page, domain.Platform(req.Platform), linkedInJob{ID: req.JobID, URL: req.Link, Title: req.Role, Company: req.Company})
	lazy := newManagerLazy(b, ctx, req)
	if err != nil {
		lazy.holdReason = err.Error()
		lazy.holdStage = "listing_read"
		return lazy, err
	}
	lazy.jobDesc = result.Description
	return lazy, nil
}

func (m *Manager) persistSubmissionReadHold(ctx context.Context, userID string, req SubmitRequest, readErr error) {
	pack := documents.ParseApplicationPackJSON(req.DocumentRefsJSON)
	pack.HoldReason, pack.HoldStage = readErr.Error(), "listing_read"
	raw := documents.WriteApplicationPackJSON(pack)
	if raw == "" {
		return
	}
	// A concurrent picker update owns its newer pack. Do not replace it with
	// the read failure based on this attempt's earlier selection.
	if _, err := m.db.ExecContext(ctx, "UPDATE jobs_pending_review SET document_refs_json=? WHERE job_id=? AND user_id=? AND COALESCE(document_refs_json,'')=?", raw, req.JobID, userID, req.DocumentRefsJSON); err != nil {
		logReadHoldFailure(err)
	}
}

func logReadHoldFailure(err error) {
	log.Warn().Err(err).Msg("approve: could not persist listing read hold")
}

type capturedJobPage struct {
	jobpage.Page
	latest *jobpage.Snapshot
}

type readOnlyBrowserContextKey struct{}

func readOnlyBrowserRequest(ctx context.Context) bool {
	value, _ := ctx.Value(readOnlyBrowserContextKey{}).(bool)
	return value
}

func (p *capturedJobPage) Capture(ctx context.Context, t jobpage.Target, l jobpage.Limits) (jobpage.Snapshot, error) {
	s, e := p.Page.Capture(ctx, t, l)
	if e == nil {
		p.latest = &s
	}
	return s, e
}
func (b *Bot) readJobPage(ctx context.Context, page *rod.Page, platform domain.Platform, job linkedInJob) (jobpage.Result, error) {
	target, _, err := jobpage.NormalizeTarget(jobpage.Target{Platform: string(platform), URL: job.URL, Title: job.Title, Company: job.Company, UserID: b.cfg.UserID, RunID: b.cfg.AutomationRunID, RecordID: job.ID})
	if err != nil {
		return jobpage.Result{}, err
	}
	ctx = llm.AttachAttribution(ctx, domain.LLMCallContext{UserID: target.UserID, AutomationRunID: target.RunID, JobID: target.RecordID, Platform: target.Platform})
	baseline := jobpage.Baseline{}
	if platform == domain.PlatformLinkedIn {
		baseline.Known = func(ctx context.Context, t jobpage.Target, l jobpage.Limits) (jobpage.Result, error) {
			r, err := readLinkedInDescription(ctx, page, linkedInJob{ID: t.JobID, URL: t.URL, Title: t.Title, Company: t.Company})
			result := jobpage.Result{Target: t, Identity: pageIdentity(r, t), Description: r.Description}
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				code := jobpage.FailureMissing
				if errors.Is(err, errLinkedInIdentity) {
					code = jobpage.FailureIdentity
				}
				return result, &jobpage.ReadError{Code: code, Cause: err}
			}
			return result, nil
		}
		baseline.Verify = func(ctx context.Context, t jobpage.Target) (jobpage.Identity, error) {
			r, err := evalLinkedInJob(page.Context(ctx), t.JobID, false, false, linkedInJob{ID: t.JobID, URL: t.URL, Title: t.Title, Company: t.Company})
			if err != nil {
				return jobpage.Identity{}, err
			}
			return pageIdentity(r, t), nil
		}
	}
	reader := b.cfg.JobPageReader
	if reader == nil {
		reader = jobpage.NewReader(nil, nil, nil)
	}
	observed := &capturedJobPage{Page: jobpage.NewBrowserPage(page, baseline)}
	result, err := reader.Read(ctx, observed, target)
	if err != nil && b.cfg.BrowserMeter != nil && b.cfg.BrowserMeter.Err() != nil {
		err = b.cfg.BrowserMeter.Err()
	}
	if err != nil {
		var readErr *jobpage.ReadError
		if !errors.As(err, &readErr) {
			code := jobpage.ErrorCode(err)
			if code == "" {
				code = jobpage.FailureVerification
			}
			err = &jobpage.ReadError{Code: code, Cause: err}
		}
	}
	if b.cfg.JobPageStore != nil {
		_, scope, _ := jobpage.NormalizeTarget(target)
		failure := jobpage.ErrorCode(err)
		if failure == "" && err != nil {
			failure = jobpage.FailureVerification
		}
		telemetryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var capture *jobpage.Snapshot
		if c, e := b.cfg.JobPageStore.GetConfig(telemetryCtx); e == nil && c.PrivateCaptures {
			capture = observed.latest
		}
		_, persistErr := b.cfg.JobPageStore.RecordRead(telemetryCtx, jobpage.ReadEvent{UserID: target.UserID, RunID: target.RunID, RecordID: target.RecordID, Scope: scope, Source: result.Source, Failure: failure, RecipeID: result.RecipeID, Metrics: result.Metrics}, capture)
		cancel()
		if persistErr != nil {
			b.log.Error().Err(persistErr).Msg("job page: read accounting failed")
			if err == nil {
				err = &jobpage.ReadError{Code: jobpage.FailurePersistence, Cause: persistErr}
			}
		}
	}
	b.log.Info().Str("job_id", job.ID).Str("platform", target.Platform).Str("source", string(result.Source)).Str("failure_code", string(jobpage.ErrorCode(err))).Interface("reading", result.Metrics).Msg("job page: read finished")
	return result, err
}

func (b *Bot) readAdaptiveLinkedInDescription(ctx context.Context, page *rod.Page, job linkedInJob) (linkedInResolution, error) {
	if b.cfg.JobPageReader == nil {
		return readLinkedInDescription(ctx, page, job)
	}
	result, err := b.readJobPage(ctx, page, domain.PlatformLinkedIn, job)
	if err != nil {
		return linkedInResolution{}, err
	}
	// Application-control authority remains in the native resolver. The page
	// model supplies description selection only.
	r, err := evalLinkedInJob(page.Context(ctx), linkedInExpectedID(job.URL), false, false, job)
	if err != nil {
		return r, err
	}
	if !r.IdentityVerified {
		return r, r.resolutionError()
	}
	r.Description = result.Description
	return r, nil
}

// FetchJobPage reads supported boards in the authenticated worker without
// entering any application flow. Other sources retain the existing HTTP reader.
func (m *Manager) ReadJobPage(ctx context.Context, userID, rawURL string) (jobpage.Result, error) {
	target, _, err := jobpage.NormalizeTarget(jobpage.Target{URL: rawURL, UserID: userID})
	if err != nil {
		return jobpage.Result{}, err
	}

	if m.pageReader == nil || m.pageStore == nil {
		return jobpage.Result{}, errors.New("verified page reader unavailable")
	}
	cfg, err := m.pageStore.GetConfig(ctx)
	if err != nil {
		return jobpage.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Limits.WallClockSeconds)*time.Second)
	defer cancel()
	allow := false
	if m.userTypeChecker != nil {
		a, t := m.userTypeChecker(userID)
		allow = a || t
	}
	gs := config.ResolveOperationalSettings(m.cfgStore, userID, allow)
	b := New(Config{JobPageReader: m.pageReader, JobPageStore: m.pageStore, UserID: userID, AutomationRunID: uuid.NewString()})
	if m.browserMeterSvc != nil {
		inc, e := m.browserMeterSvc.BrowserIncrementDuration()
		if e != nil {
			return jobpage.Result{}, e
		}
		b.cfg.BrowserMeter = NewLiveBrowserMeter(m.browserMeterSvc, userID, b.cfg.AutomationRunID, inc, nil)
		defer b.cfg.BrowserMeter.Stop()
		e = preflightReadMeter(ctx, b.cfg.BrowserMeter)
		if e != nil {
			return jobpage.Result{}, e
		}
	}
	var br *rod.Browser
	ctx = context.WithValue(ctx, readOnlyBrowserContextKey{}, true)
	if target.Platform == "seek" {
		br, err = m.getSeekBrowser(userID, gs, ctx)
	} else {
		br, err = m.getLinkedInBrowser(userID, gs, ctx)
	}
	if err != nil {
		return jobpage.Result{}, &jobpage.ReadError{Code: jobpage.FailureBrowser, Cause: err}
	}
	page, err := br.Context(ctx).Page(proto.TargetCreateTarget{URL: target.URL})
	if err != nil {
		return jobpage.Result{}, &jobpage.ReadError{Code: jobpage.FailureBrowser, Cause: err}
	}
	defer func() { _ = page.Context(context.WithoutCancel(ctx)).Timeout(3 * time.Second).Close() }()
	ctx, err = activateReadMeter(ctx, b.cfg.BrowserMeter, target.JobID)
	if err != nil {
		return jobpage.Result{}, err
	}
	page = page.Context(ctx)
	_ = page.Context(ctx).WaitLoad()
	result, err := b.readJobPage(ctx, page, domain.Platform(target.Platform), linkedInJob{ID: target.JobID, URL: target.URL})
	if err != nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) FetchJobPage(ctx context.Context, userID, rawURL string) (string, error) {
	if _, _, err := jobpage.NormalizeTarget(jobpage.Target{URL: rawURL}); err != nil {
		p, e := detectPlatformFromURL(rawURL)
		if e == nil && (p == domain.PlatformLinkedIn || p == domain.PlatformSeek) {
			return "", err
		}
		return resume.FetchJobPage(ctx, rawURL)
	}
	result, err := m.ReadJobPage(ctx, userID, rawURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Job title: %s\nCompany: %s\n\n%s", result.Identity.Title, result.Identity.Company, result.Description), nil
}
