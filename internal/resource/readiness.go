package resource

import (
	"math"

	"github.com/user/jobifai/internal/domain"
)

// ComputeReadiness derives a structured readiness verdict from explicit inputs.
// It uses PriceResource to check effective credit economics — no pricing formula is
// duplicated here. qd is the current shared quota defaults (CreditsPerUSD, ServiceMarkup).
func ComputeReadiness(cfg domain.ResourceMeteringDefaults, persisted bool, qd domain.QuotaDefaults) domain.ResourceMeteringReadiness {
	var r domain.ResourceMeteringReadiness
	r.Warnings = []string{}

	r.DefaultsPersisted = persisted
	r.MasterEnabled = cfg.Enabled

	if !persisted {
		r.Ready = false
		r.Status = domain.ResourceMeteringStatusUnreviewed
		r.Warnings = append(r.Warnings,
			"Resource metering configuration has never been saved. "+
				"Built-in safe defaults (metering disabled) are in use. "+
				"Review and save Resource metering settings before production launch.")
		r.BrowserAutomation = browserReadiness(cfg, qd)
		r.DocumentRender = renderReadiness(cfg, qd)
		return r
	}

	if !cfg.Enabled {
		r.Ready = true
		r.Status = domain.ResourceMeteringStatusDisabled
		r.BrowserAutomation = domain.ResourceBrowserReadiness{Enabled: cfg.Browser.Enabled}
		r.DocumentRender = domain.ResourceRenderReadiness{Enabled: cfg.Render.Enabled}
		return r
	}

	// Master is enabled — evaluate each resource type.
	br := browserReadiness(cfg, qd)
	rr := renderReadiness(cfg, qd)

	overallReady := (!cfg.Browser.Enabled || br.Ready) && (!cfg.Render.Enabled || rr.Ready)

	if overallReady {
		r.Ready = true
		r.Status = domain.ResourceMeteringStatusReady
	} else {
		r.Ready = false
		r.Status = domain.ResourceMeteringStatusInvalid
		if cfg.Browser.Enabled && !br.Ready {
			r.Warnings = append(r.Warnings, browserWarning(cfg))
		}
		if cfg.Render.Enabled && !rr.Ready {
			r.Warnings = append(r.Warnings, renderWarning(cfg, qd))
		}
	}

	r.BrowserAutomation = br
	r.DocumentRender = rr
	return r
}

func browserReadiness(cfg domain.ResourceMeteringDefaults, qd domain.QuotaDefaults) domain.ResourceBrowserReadiness {
	b := cfg.Browser
	inc := b.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	br := domain.ResourceBrowserReadiness{
		Enabled:                b.Enabled,
		RawCostUSDMicroPerHour: b.RawCostUSDMicroPerHour,
		BillingIncrementSecs:   inc,
	}
	if !b.Enabled {
		br.Ready = true // explicitly disabled — not a readiness problem
		return br
	}
	pricing := PriceResource(cfg, qd, domain.ResourceTypeBrowserAutomation)
	if pricing.Credits > 0 {
		br.Ready = true
		// credits/minute = incrementCredits × (60 / incrementSecs)
		br.CreditsPerMinute = math.Round(float64(pricing.Credits)*(60.0/float64(inc))*100) / 100
	}
	return br
}

func renderReadiness(cfg domain.ResourceMeteringDefaults, qd domain.QuotaDefaults) domain.ResourceRenderReadiness {
	rn := cfg.Render
	rr := domain.ResourceRenderReadiness{
		Enabled:                  rn.Enabled,
		RawCostUSDMicroPerRender: rn.RawCostUSDMicroPerRender,
	}
	if !rn.Enabled {
		rr.Ready = true // explicitly disabled — not a readiness problem
		return rr
	}
	pricing := PriceResource(cfg, qd, domain.ResourceTypeDocumentRender)
	if pricing.Credits > 0 {
		rr.Ready = true
		rr.CreditsPerRender = pricing.Credits
	}
	return rr
}

func browserWarning(cfg domain.ResourceMeteringDefaults) string {
	b := cfg.Browser
	if b.RawCostUSDMicroPerHour <= 0 {
		return "Browser automation is enabled but raw cost/hour is zero — no credits will be charged."
	}
	return "Browser automation is enabled but its configured economics produce zero credits. " +
		"Check CreditsPerUSD and ServiceMarkup in quota defaults."
}

func renderWarning(cfg domain.ResourceMeteringDefaults, qd domain.QuotaDefaults) string {
	r := cfg.Render
	if r.RawCostUSDMicroPerRender <= 0 {
		return "Document rendering is enabled but raw cost/render is zero — no credits will be charged."
	}
	_ = qd
	return "Document rendering is enabled but its configured economics produce zero credits. " +
		"Check CreditsPerUSD and ServiceMarkup in quota defaults."
}
