package bot

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func (b *Bot) policies() domain.DocumentPolicies {
	gs := b.cfg.Settings
	config.EnsureDocumentPolicies(&gs)
	return gs.DocumentPolicies
}

func (b *Bot) effectiveRenderContext() documents.RenderContext {
	market := b.cfg.Settings.DefaultResumeMarket
	if b.cfg.Settings.DocumentPolicies.RegionalDefaults != nil {
		if m, ok := b.cfg.Settings.DocumentPolicies.RegionalDefaults[market]; ok && m != "" {
			market = m
		}
	}
	return documents.RenderContext{
		Market:   market,
		Language: "en",
	}
}

func (b *Bot) resolveApplicationDocs(overrides documents.ApplicationDocumentOverrides, caps documents.FormDocumentCapabilities) documents.ResolveResult {
	var r documents.Resolver
	policies := b.policies()
	def := documents.DefaultsView{}
	if b.cfg.Documents != nil {
		if list, err := b.cfg.Documents.List(context.Background(), b.cfg.UserID); err == nil {
			def = list.Defaults
		}
	}
	hasProfile := b.currentProfile() != nil
	hasResumeDefault := def.ResumeVersionID != ""
	hasCoverDefault := def.CoverLetterVersionID != ""
	return r.Resolve(documents.ResolveInput{
		Policies:            policies,
		Defaults:            def,
		Overrides:           overrides,
		Caps:                caps,
		EffectiveMarket:     b.effectiveRenderContext().Market,
		HasConfirmedProfile: hasProfile,
		DefaultResumeExists: hasResumeDefault,
		DefaultCoverExists:  hasCoverDefault,
	})
}

func (l *lazyDocGen) materializeWithPolicies() {
	if l.b.cfg.Documents == nil {
		l.resume, l.cover = l.b.generateDocs(l.ctx, l.job, l.jobDesc)
		return
	}
	caps := l.formCaps
	if caps.ResumeFileSlots == 0 {
		caps.ResumeFileSlots = 2
	}
	overrides := documents.ApplicationDocumentOverrides{
		ResumeVersionID: l.resumeVersionOverride,
		CoverVersionID:  l.coverVersionOverride,
		Frozen:          l.frozen,
	}
	res := l.b.resolveApplicationDocs(overrides, caps)
	if res.Hold {
		l.holdReason = res.HoldReason
		log.Warn().Str("job", l.job.Title).Str("reason", res.HoldReason).Msg("documents: hold — will not substitute")
		return
	}
	rc := l.b.effectiveRenderContext()
	pack, err := l.b.cfg.Documents.MaterializeApplicationDocs(
		l.ctx, l.b.cfg.UserID, l.job.Company, l.job.Title, l.jobDesc, res, rc, l.b.currentProfile(),
		documents.MaterializeDeps{
			TailorProfile: func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error) {
				if l.b.cfg.Tailor == nil {
					return profile, nil
				}
				market := l.b.loadMarket()
				promptCtx := jobDesc
				if market != nil && market.TailoredPrompt != "" {
					promptCtx = market.TailoredPrompt + "\n\nJob Description:\n" + jobDesc
				}
				return l.b.cfg.Tailor.TailorProfile(l.b.llmCtx(ctx, "tailor resume", l.job.ID), profile, promptCtx)
			},
			WriteCover: func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error) {
				if l.b.cfg.Tailor == nil {
					return "", nil
				}
				market := l.b.loadMarket()
				promptCtx := jobDesc
				if market != nil && market.CoverLetterPrompt != "" {
					promptCtx = market.CoverLetterPrompt + "\n\nJob Description:\n" + jobDesc
				}
				return l.b.cfg.Tailor.WriteCoverLetter(l.b.llmCtx(ctx, "cover letter", l.job.ID), profile, promptCtx)
			},
			WritePDF: func(company, title, kind string, pdf []byte) string {
				path, err := documents.ExportPDFToJobDir(".", company, title, kind, pdf)
				if err != nil {
					log.Warn().Err(err).Msg("documents: export pdf for upload")
					return ""
				}
				return path
			},
		},
	)
	if err != nil {
		if isJobifaiQuotaExceeded(err) || isProviderUsageLimit(err) {
			l.holdReason = llmAbortReason(err)
		} else {
			l.holdReason = err.Error()
		}
		return
	}
	if pack.Hold {
		l.holdReason = pack.HoldReason
		return
	}
	l.resume = pack.ResumePath
	l.cover = pack.CoverPath
	l.resumeVersionID = pack.ResumeVersionID
	l.coverVersionID = pack.CoverVersionID
	l.refsJSON = documents.WriteRefsJSON(pack.Refs)
	if res.Resume.UseSite {
		l.useSiteResume = true
	}
}

func (l *lazyDocGen) policyBlocked() bool {
	return strings.TrimSpace(l.holdReason) != ""
}

func (l *lazyDocGen) prepareForReview() {
	l.formCaps = documents.FormDocumentCapabilities{ResumeFileSlots: 2, CoverOptional: true}
	l.once.Do(l.materializeWithPolicies)
}
