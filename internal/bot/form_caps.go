package bot

import (
	"strings"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/documents"
)

func capsFromFormFields(fields []formField) documents.FormDocumentCapabilities {
	caps := documents.FormDocumentCapabilities{Detected: true}
	for _, f := range fields {
		if f.Type != "file" {
			continue
		}
		caps.ResumeFileSlots++
		q := strings.ToLower(f.Question)
		if strings.Contains(q, "cover") {
			if strings.Contains(q, "optional") {
				caps.CoverOptional = true
			} else {
				caps.CoverRequired = true
			}
		}
	}
	if caps.ResumeFileSlots >= 2 && !caps.CoverRequired {
		caps.CoverOptional = true
	}
	return caps
}

func mergeCaps(dst *documents.FormDocumentCapabilities, src documents.FormDocumentCapabilities) {
	if !src.Detected {
		return
	}
	dst.Detected = true
	if src.ResumeFileSlots > dst.ResumeFileSlots {
		dst.ResumeFileSlots = src.ResumeFileSlots
	}
	dst.CoverRequired = dst.CoverRequired || src.CoverRequired
	dst.CoverOptional = dst.CoverOptional || src.CoverOptional
	if src.SiteResumePresent {
		dst.SiteResumePresent = true
	}
	if src.SiteResumeAmbiguous {
		dst.SiteResumeAmbiguous = true
	}
}

func detectSiteResumeOnField(f formField, siteMode bool) (present, ambiguous bool) {
	if f.Type != "file" {
		return false, false
	}
	if !f.HasFile {
		return false, false
	}
	if siteMode {
		return true, false
	}
	// Platform pre-filled resume while we intended to upload — ambiguous substitution risk.
	if f.Question != "" {
		return true, true
	}
	return true, false
}

func probeLinkedInApplyCaps(page *rod.Page) documents.FormDocumentCapabilities {
	res, err := page.Eval(`() => {
		const files = [...document.querySelectorAll('input[type=file]')];
		let coverRequired = false, coverOptional = false, siteResume = false;
		for (const el of files) {
			const label = (el.closest('label')?.innerText || el.getAttribute('aria-label') || '').toLowerCase();
			if (label.includes('cover')) {
				if (label.includes('optional')) coverOptional = true; else coverRequired = true;
			}
			if (el.files && el.files.length > 0) siteResume = true;
		}
		return { slots: files.length, coverRequired, coverOptional, siteResume };
	}`)
	if err != nil {
		return documents.FormDocumentCapabilities{}
	}
	v := res.Value
	caps := documents.FormDocumentCapabilities{
		Detected:          v.Get("slots").Int() > 0,
		ResumeFileSlots:   v.Get("slots").Int(),
		CoverRequired:     v.Get("coverRequired").Bool(),
		CoverOptional:     v.Get("coverOptional").Bool(),
		SiteResumePresent: v.Get("siteResume").Bool(),
	}
	if caps.ResumeFileSlots >= 2 && !caps.CoverRequired {
		caps.CoverOptional = true
	}
	return caps
}
