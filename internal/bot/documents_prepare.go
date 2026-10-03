package bot

import (
	"context"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func (b *Bot) policiesRequireVerifiedDocuments() bool {
	if b.cfg.Documents == nil {
		return false
	}
	p := b.policies()
	if p.ResumeMode != domain.ResumeDocumentModeSiteHosted {
		return true
	}
	switch p.CoverMode {
	case domain.CoverDocumentModeWhenRequired, domain.CoverDocumentModeWhenAccepted, domain.CoverDocumentModeGeneralDefault:
		return true
	default:
		return false
	}
}

func (b *Bot) policies() domain.DocumentPolicies {
	gs := b.cfg.Settings
	config.EnsureDocumentPolicies(&gs)
	return gs.DocumentPolicies
}

func (b *Bot) effectiveRenderContext() documents.RenderContext {
	return documents.RenderContext{
		Market:   b.cfg.Settings.DefaultResumeMarket,
		Language: "en",
	}
}

func (b *Bot) effectiveResumeVersionID(def documents.DefaultsView) string {
	market := strings.TrimSpace(b.cfg.Settings.DefaultResumeMarket)
	if b.cfg.Settings.DocumentPolicies.RegionalDefaults != nil {
		if vid := strings.TrimSpace(b.cfg.Settings.DocumentPolicies.RegionalDefaults[market]); vid != "" {
			return vid
		}
	}
	return def.ResumeVersionID
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
	hasResumeDefault := b.effectiveResumeVersionID(def) != "" || def.ResumeVersionID != ""
	hasCoverDefault := def.CoverLetterVersionID != ""
	return r.Resolve(documents.ResolveInput{
		Policies:                 policies,
		Defaults:                 def,
		EffectiveResumeVersionID: b.effectiveResumeVersionID(def),
		Overrides:                overrides,
		Caps:                     caps,
		EffectiveMarket:          b.effectiveRenderContext().Market,
		HasConfirmedProfile:      hasProfile,
		DefaultResumeExists:      hasResumeDefault,
		DefaultCoverExists:       hasCoverDefault,
	})
}

func (l *lazyDocGen) restoreFrozenPack() {
	if !l.frozen || l.packRestored {
		return
	}
	pack := documents.ParseApplicationPackJSON(l.refsJSON)
	if pack.HoldReason != "" {
		l.holdReason = pack.HoldReason
	}
	if pack.Resume.Outcome == domain.DocumentOutcomeSiteHosted {
		l.useSiteResume = true
	}
	if pack.Resume.LocalPath != "" {
		l.resume = pack.Resume.LocalPath
	} else if l.resumeOverride != "" {
		l.resume = l.resumeOverride
	}
	if pack.Cover.LocalPath != "" {
		l.cover = pack.Cover.LocalPath
	} else if l.coverOverride != "" {
		l.cover = l.coverOverride
	}
	if pack.Resume.ContentVersionID != "" {
		l.resumeVersionID = pack.Resume.ContentVersionID
	}
	if pack.Cover.ContentVersionID != "" {
		l.coverVersionID = pack.Cover.ContentVersionID
	}
	if pack.Selection.ResumeVersionID != "" && l.resumeVersionOverride == "" {
		l.resumeVersionOverride = pack.Selection.ResumeVersionID
	}
	if pack.Selection.CoverVersionID != "" && l.coverVersionOverride == "" {
		l.coverVersionOverride = pack.Selection.CoverVersionID
	}
	l.resumeUseSiteOverride = pack.Selection.ResumeUseSite
	l.coverSkipOverride = pack.Selection.CoverSkip
	l.pack = pack
	l.packRestored = true
}

func (l *lazyDocGen) materializeWithPolicies() {
	if l.b.cfg.Documents == nil {
		l.resume, l.cover = l.b.generateDocs(l.ctx, l.job, l.jobDesc)
		return
	}
	if !l.formCaps.Detected {
		l.holdReason = "form document requirements unknown — open apply form before preparing documents"
		return
	}
	overrides := l.applicationDocumentOverrides()
	res := l.b.resolveApplicationDocs(overrides, l.formCaps)
	if res.Hold {
		l.holdReason = res.HoldReason
		l.pack = documents.PackFromPrepared(l.holdReason, res.Resume, res.Cover, "", "")
		l.refsJSON = documents.WriteApplicationPackJSON(l.pack)
		log.Warn().Str("job", l.job.Title).Str("reason", res.HoldReason).Msg("documents: hold")
		return
	}
	rc := l.b.effectiveRenderContext()
	exportRoot := "."
	if wd, err := os.Getwd(); err == nil {
		exportRoot = wd
	}
	pack, err := l.b.cfg.Documents.MaterializeApplicationDocs(
		l.ctx, l.b.cfg.UserID, l.job.ID, l.job.Company, l.job.Title, l.jobDesc, res, l.b.policies(), rc, l.b.currentProfile(),
		documents.MaterializeDeps{
			TailorProfile: func(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error) {
				if l.b.cfg.Tailor == nil {
					return nil, context.Canceled
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
					return "", context.Canceled
				}
				market := l.b.loadMarket()
				promptCtx := jobDesc
				if market != nil && market.CoverLetterPrompt != "" {
					promptCtx = market.CoverLetterPrompt + "\n\nJob Description:\n" + jobDesc
				}
				return l.b.cfg.Tailor.WriteCoverLetter(l.b.llmCtx(ctx, "cover letter", l.job.ID), profile, promptCtx)
			},
			ExportUpload: func(userID, jobID, versionID, kind string, payload documents.VersionUploadPayload) (string, error) {
				return documents.ExportApplicationUploadFile(exportRoot, userID, jobID, versionID, kind, payload.Filename, payload.Data)
			},
		},
	)
	if err != nil {
		if isJobifaiQuotaExceeded(err) || isProviderUsageLimit(err) {
			l.holdReason = llmAbortReason(err)
		} else {
			l.holdReason = err.Error()
		}
		l.absorbPartialMaterialization(pack, res)
		l.pack = documents.PackFromPrepared(l.holdReason, pack.Resume, pack.Cover, l.resume, l.cover)
		l.mergeMaterializedPackPaths()
		l.refsJSON = documents.WriteApplicationPackJSON(l.pack)
		return
	}
	if pack.Hold {
		l.holdReason = pack.HoldReason
		l.pack = pack.Pack
		l.refsJSON = documents.WriteApplicationPackJSON(l.pack)
		return
	}
	l.resume = pack.ResumePath
	l.cover = pack.CoverPath
	l.resumeVersionID = pack.ResumeVersionID
	l.coverVersionID = pack.CoverVersionID
	l.pack = pack.Pack
	l.refsJSON = documents.WriteApplicationPackJSON(l.pack)
	if res.Resume.UseSite {
		l.useSiteResume = true
	}
}

func (l *lazyDocGen) ensureMaterialized() {
	if l.frozen {
		l.restoreFrozenPack()
		return
	}
	if !l.formCaps.Detected {
		return
	}
	l.matMu.Lock()
	defer l.matMu.Unlock()
	if l.materialized {
		return
	}
	l.materializeWithPolicies()
	if !l.formCaps.Detected {
		return
	}
	if strings.Contains(l.holdReason, "open apply form before preparing") {
		return
	}
	l.materialized = true
}

func (l *lazyDocGen) policyBlocked() bool {
	return strings.TrimSpace(l.holdReason) != ""
}

func (l *lazyDocGen) applicationDocumentOverrides() documents.ApplicationDocumentOverrides {
	o := documents.ApplicationDocumentOverrides{
		ResumeVersionID: l.resumeVersionOverride,
		CoverVersionID:  l.coverVersionOverride,
		ResumeUseSite:   l.resumeUseSiteOverride,
		CoverSkip:       l.coverSkipOverride,
		Frozen:          l.frozen,
	}
	if o.ResumeVersionID == "" && l.resumeVersionID != "" {
		o.ResumeVersionID = l.resumeVersionID
	}
	if o.CoverVersionID == "" && l.coverVersionID != "" {
		o.CoverVersionID = l.coverVersionID
	}
	return o
}

func (l *lazyDocGen) absorbPartialMaterialization(pack documents.PreparedApplicationDocs, res documents.ResolveResult) {
	if pack.ResumePath != "" {
		l.resume = pack.ResumePath
	}
	if pack.ResumeVersionID != "" {
		l.resumeVersionID = pack.ResumeVersionID
	} else if pack.Resume.VersionID != "" {
		l.resumeVersionID = pack.Resume.VersionID
	}
	if pack.CoverPath != "" {
		l.cover = pack.CoverPath
	}
	if pack.CoverVersionID != "" {
		l.coverVersionID = pack.CoverVersionID
	} else if pack.Cover.VersionID != "" {
		l.coverVersionID = pack.Cover.VersionID
	}
}

func (l *lazyDocGen) mergeMaterializedPackPaths() {
	if l.resume != "" {
		l.pack.Resume.LocalPath = l.resume
		if l.resumeVersionID != "" {
			l.pack.Resume.ContentVersionID = l.resumeVersionID
		}
	}
	if l.cover != "" {
		l.pack.Cover.LocalPath = l.cover
		if l.coverVersionID != "" {
			l.pack.Cover.ContentVersionID = l.coverVersionID
		}
	}
}

func (l *lazyDocGen) invalidateMaterialization(prevCaps documents.FormDocumentCapabilities) {
	l.matMu.Lock()
	defer l.matMu.Unlock()
	l.materialized = false
	if strings.Contains(l.holdReason, "unknown") || strings.Contains(l.holdReason, "open apply form before preparing") {
		l.holdReason = ""
		l.resume = ""
		l.cover = ""
		l.resumeVersionID = ""
		l.coverVersionID = ""
		l.useSiteResume = false
		l.pack = documents.ApplicationDocumentPack{}
		l.refsJSON = ""
		return
	}
	coverNewlyRequired := !prevCaps.CoverRequired && !prevCaps.CoverOptional &&
		(l.formCaps.CoverRequired || l.formCaps.CoverOptional)
	if coverNewlyRequired {
		l.cover = ""
		l.coverVersionID = ""
		if l.pack.Cover.LocalPath != "" || l.pack.Cover.ContentVersionID != "" {
			l.pack.Cover = domain.ApplicationDocumentRef{Kind: documents.KindCoverLetter}
		}
	}
}

func (l *lazyDocGen) applyCaps(caps documents.FormDocumentCapabilities) {
	prevCaps := l.formCaps
	prev := capsFingerprint(l.formCaps)
	wasDetected := l.formCaps.Detected
	mergeCaps(&l.formCaps, caps)
	next := capsFingerprint(l.formCaps)
	if l.frozen {
		l.capsFingerprint = next
		return
	}
	shouldReset := l.materialized && l.formCaps.Detected && (
		(prev != "" && prev != next) ||
		(!wasDetected && l.formCaps.Detected) ||
		strings.Contains(l.holdReason, "unknown"))
	if shouldReset {
		l.invalidateMaterialization(prevCaps)
	}
	l.capsFingerprint = next
}

func (l *lazyDocGen) packJSONForPersist() string {
	if l.refsJSON != "" {
		return l.refsJSON
	}
	return documents.WriteApplicationPackJSON(l.pack)
}
