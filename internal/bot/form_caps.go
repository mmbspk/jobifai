package bot

import (
	"fmt"
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
		switch f.DocumentKind {
		case documents.KindCoverLetter:
			if f.Required {
				caps.CoverRequired = true
			} else {
				caps.CoverOptional = true
			}
		case documents.KindResume:
			caps.ResumeFileSlots++
		default:
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
	}
	if caps.ResumeFileSlots >= 2 && !caps.CoverRequired && !caps.CoverOptional {
		caps.CoverOptional = true
	}
	return caps
}

func capsFingerprint(c documents.FormDocumentCapabilities) string {
	if !c.Detected {
		return ""
	}
	return fmt.Sprintf("d:%d:r:%v:o:%v:site:%v:amb:%v",
		c.ResumeFileSlots, c.CoverRequired, c.CoverOptional, c.SiteResumePresent, c.SiteResumeAmbiguous)
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
	if f.DocumentKind == documents.KindCoverLetter {
		return false, false
	}
	if !f.HasFile {
		return false, false
	}
	if siteMode {
		return true, false
	}
	if f.Question != "" {
		return true, true
	}
	return true, false
}

func probeApplyFormCaps(page *rod.Page) documents.FormDocumentCapabilities {
	res, err := page.Eval(`() => {
		function classifyDocumentKind(text) {
			const q = (text || '').toLowerCase();
			if (q.includes('cover')) return 'cover_letter';
			if (q.includes('resume') || q.includes('résumé') || q.includes('cv')) return 'resume';
			return 'unknown';
		}
		function labelFor(id) {
			if (!id) return null;
			return [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === id) || null;
		}
		const files = [...document.querySelectorAll('input[type=file]')];
		let resumeSlots = 0, coverRequired = false, coverOptional = false, siteResume = false;
		for (const el of files) {
			const lbl = labelFor(el.id) || el.closest('label');
			const question = lbl ? lbl.textContent.trim() : (el.getAttribute('aria-label') || '');
			const kind = classifyDocumentKind(question);
			const required = el.required || el.getAttribute('aria-required') === 'true';
			if (kind === 'cover_letter') {
				if (required) coverRequired = true; else coverOptional = true;
			} else {
				resumeSlots++;
			}
			if (el.files && el.files.length > 0 && kind !== 'cover_letter') siteResume = true;
		}
		return { resumeSlots, coverRequired, coverOptional, siteResume, detected: files.length > 0 };
	}`)
	if err != nil {
		return documents.FormDocumentCapabilities{}
	}
	v := res.Value
	caps := documents.FormDocumentCapabilities{
		Detected:          v.Get("detected").Bool(),
		ResumeFileSlots:   v.Get("resumeSlots").Int(),
		CoverRequired:     v.Get("coverRequired").Bool(),
		CoverOptional:     v.Get("coverOptional").Bool(),
		SiteResumePresent: v.Get("siteResume").Bool(),
	}
	if caps.ResumeFileSlots >= 2 && !caps.CoverRequired && !caps.CoverOptional {
		caps.CoverOptional = true
	}
	return caps
}
