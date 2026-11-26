package bot

// form.go, generic form-field detection and filling used by both LinkedIn Easy
// Apply and Seek Quick Apply.  Works by injecting JS into the live rod page to
// scan the current step, then filling each unanswered field via profile defaults,
// heuristics, or an LLM call.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// ── Data types ────────────────────────────────────────────────────────────────

// errUploadIdentityUnverifiable is returned by linkedInWaitForResumeAccepted when the
// uploaded resume cannot be confirmed as the intended file (e.g. checked radio label
// does not match the expected filename). Triggers a HOLD rather than a hard failure.
var errUploadIdentityUnverifiable = errors.New(
	"linkedin: resume upload identity could not be verified — please confirm selection manually")

// Stable stage codes for upload-verification holds. Persisted as structured diagnostic
// metadata (pack.HoldStage) so the exact failure point survives in durable storage; the
// human-readable text stays in errUploadIdentityUnverifiable.Error() / pack.HoldReason.
const (
	uploadStageSectionNotFound     = "pre_upload_section_not_found"
	uploadStageNoRadios            = "pre_upload_no_radios"
	uploadStageAcceptedNotVisible  = "accepted_filename_not_visible"
	uploadStageVersionScopedNotSel = "pre_existing_version_scoped_not_selected"
	uploadStageNotVersionScoped    = "pre_existing_filename_not_version_scoped"
	uploadStageNoResumeGroup       = "upload_no_resume_group"
	uploadStagePostUploadMismatch  = "post_upload_label_mismatch"
	uploadStageAcceptanceTimeout   = "upload_acceptance_timeout"
	uploadStageFinalReviewMismatch = "final_review_filename_mismatch"
)

// uploadIdentityHold wraps errUploadIdentityUnverifiable with the stable stage code and the
// intended filename that produced the hold. Unwrap keeps errors.Is(err,
// errUploadIdentityUnverifiable) true, while callers extract the structured fields via
// errors.As to persist them as diagnostics.
type uploadIdentityHold struct {
	stage    string
	filename string
}

func (e *uploadIdentityHold) Error() string { return errUploadIdentityUnverifiable.Error() }
func (e *uploadIdentityHold) Unwrap() error { return errUploadIdentityUnverifiable }

type formFieldOption struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Label string `json:"label"`
}

type formField struct {
	Type              string            `json:"type"`     // file | radio | select | text
	Index             int               `json:"index"`    // ordinal among same type
	Name              string            `json:"name"`     // radio group name
	ID                string            `json:"id"`       // select / text element id
	Question          string            `json:"question"` // visible label text
	Group             string            `json:"group"`    // section heading (fieldset legend, h2/h3)
	ProfileRecordPath string            `json:"profileRecordPath,omitempty"`
	HelpText          string            `json:"helpText"`        // hint / description text visible below label
	Options           []formFieldOption `json:"options"`         // radio / select choices
	HasFile           bool              `json:"hasFile"`         // file already attached
	DocumentKind      string            `json:"documentKind"`    // resume | cover_letter | unknown
	Required          bool              `json:"required"`        // file field marked required on form
	InputType         string            `json:"inputType"`       // text | number | textarea
	Typeahead         bool              `json:"isTypeahead"`     // combobox requiring dropdown selection
	UploadViaButton   bool              `json:"uploadViaButton"` // slot has no visible input; upload via button click + HandleFileDialog
	ResumeGroupName   string            `json:"resumeGroupName"` // radio group name inside the same section (empty if ambiguous or absent)
	MaxChars          int               `json:"maxChars"`        // LinkedIn SDUI character cap (maxlength or "N of M characters")
	MinValue          string            `json:"minValue,omitempty"`
	MaxValue          string            `json:"maxValue,omitempty"`
	StepValue         string            `json:"stepValue,omitempty"`
	Pattern           string            `json:"pattern,omitempty"`
	Locale            string            `json:"locale,omitempty"`
	NativeType        string            `json:"nativeType,omitempty"`
}

func (f formField) optionLabels() []string {
	labels := make([]string, len(f.Options))
	for i, o := range f.Options {
		labels[i] = o.Label
	}
	return labels
}

// ── JavaScript constants ───────────────────────────────────────────────────────

// Keep direct uploads and scanning on the same scoped, filtered file-input list.
// An ordinal among document inputs must never identify an unrelated page upload.
const jsDocumentUploadInputsFn = `
function resolveApplyFormContainer() {
 const anchor = resolveApplyContainer();
 if (!anchor) return null;
 const legacySelector = '.jobs-easy-apply-content,.jobs-easy-apply-modal';
 if (anchor.matches(legacySelector)) {
  const others = [...document.querySelectorAll(legacySelector)].filter(visibleDocumentUploadElement);
  if (others.some(el => el !== anchor && !el.contains(anchor) && !anchor.contains(el))) return null;
 }
 const isContainer = anchor.tagName === 'DIALOG' || anchor.tagName === 'FORM'
   || isNativeSeekApplyPage() || anchor.matches('.jobs-easy-apply-content,.jobs-easy-apply-modal,[data-testid],main,[role="main"],[role="dialog"]');
 return isContainer ? anchor : (anchor.closest('form') || anchor.closest('.jobs-easy-apply-content') || anchor.parentElement || anchor);
}
function visibleDocumentUploadElement(el) {
  try {
   const rect = el.getBoundingClientRect();
   if (rect.width === 0 && rect.height === 0) return false;
   if (rect.right <= 0 || rect.left >= window.innerWidth) return false;
   for (let node = el; node && node !== document.documentElement; node = node.parentElement) {
    const s = window.getComputedStyle(node);
    if (s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse' || Number(s.opacity) === 0 || node.hidden || node.hasAttribute('inert') || node.getAttribute('aria-hidden') === 'true') return false;
   }
   return true;
  } catch (e) { return false; }
}
function documentUploadInputs(container) {
 if (!container) return [];
 function containerVisible(el) {
  try {
   const group = el.closest('fieldset, [role="group"], [data-test-form-element], .artdeco-form-element, .fb-form-element') || el.parentElement?.parentElement;
   return group ? visibleDocumentUploadElement(group) : visibleDocumentUploadElement(el);
  } catch (e) { return false; }
 }
 return allInDOM(container, 'input[type="file"]').filter(el => {
  const accept = (el.accept || '').toLowerCase();
  const typeOK = !accept || accept.includes('pdf') || accept.includes('doc') || accept.includes('txt') || accept.includes('rtf');
  if (isNativeSeekApplyPage()) {
   const slot = el.closest('[data-testid="resumeFileInput"],[data-testid="coverLetterFileInput"]');
   if (slot) return typeOK && (visibleDocumentUploadElement(slot) || allInDOM(slot, 'button,a,[role="button"]').some(visibleDocumentUploadElement));
   const label = allInDOM(container, 'label[for]').find(l => l.htmlFor === el.id) || el.closest('label');
   const question = label ? label.textContent : (el.getAttribute('aria-label') || el.name || '');
   if (!/\b(resume|resum[eé]|cover|cv)\b/i.test(question)) return false;
  }
  return typeOK && containerVisible(el);
 });
}
`

// jsScanFields returns an array of unanswered form fields on the current step.
// Compatible with LinkedIn's Artdeco modal (shadow DOM) and Seek's plain-HTML form.
const jsScanFieldsInner = `
	// Walk shadow DOM recursively to find elements matching a CSS selector.
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}

	// Only consider elements that are actually painted on screen.
	// LinkedIn keeps previous-step DOM in place hidden via CSS transforms/opacity.
	// We also check viewport bounds: steps translated off-screen (translateX ±100%)
	// still have non-zero bounding rects but are outside the visible viewport area.
	function isVisible(el) {
		try {
			const rect = el.getBoundingClientRect();
			if (rect.width === 0 && rect.height === 0) return false;
			// Must not be translated off-screen horizontally (LinkedIn translateX pattern).
			// Vertical position is intentionally NOT checked — Seek's Quick Apply is a
			// scrollable full-page form and fields outside the viewport must still be detected.
			if (rect.right <= 0 || rect.left >= window.innerWidth) return false;
			let node = el;
			while (node && node !== document.documentElement) {
				const s = window.getComputedStyle(node);
				if (s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse' || Number(s.opacity) === 0 || node.hidden || node.hasAttribute('inert') || node.getAttribute('aria-hidden') === 'true') return false;
				node = node.parentElement;
			}
			return true;
		} catch(e) { return false; }
	}

	// Locate the active apply-form container using the shared resolver (defined by
	// jsLinkedInApplyContainerFn, injected before this block). Priority:
	//   1. LinkedIn native <dialog open> (new SDUI layout, identified by data-sdui-screen)
	//   2. Legacy LinkedIn class-based modal
	//   3. Seek form selectors
	// No semantic fallback: an unresolvable container means no apply form is present.
	const container = resolveApplyFormContainer();
	if (!container) return JSON.stringify([]);

	const isSeekApply = isNativeSeekApplyPage() || !!(container.closest && container.closest(
		"[data-testid='quick-apply-form'], [data-automation='apply-form'], form[data-testid*='applicationForm']"
	));

	// Use htmlFor property to handle IDs with special characters like ":" (LinkedIn React IDs)
	function labelFor(id) {
		if (!id) return null;
		return allInDOM(container, 'label[for]').find(l => l.htmlFor === id) || null;
	}

` + jsFormCharacterLimitFn + `

	function sectionGroupFor(el) {
		let node = el;
		for (let depth = 0; depth < 12 && node && node !== document.body; depth++, node = node.parentElement) {
			if (node !== el && node.tagName === 'FIELDSET') {
				const legends = [...node.children].filter(child => child.tagName === 'LEGEND');
				if (legends.length === 1) {
					const text = legends[0].textContent.trim().replace(/\s+/g, ' ');
					if (text) return text;
				}
			}
			// role=group with aria-labelledby is authoritative.
			if (node !== el && node.getAttribute('role') === 'group') {
				const lid = node.getAttribute('aria-labelledby');
				if (lid) {
					const lbl = document.getElementById(lid);
					if (lbl) return lbl.textContent.trim().replace(/\s+/g, ' ');
				}
			}
			const parent = node.parentElement;
			if (!parent) break;
			// Collect the LAST heading sibling before this node — the closest section.
			// Two fields under different headings that are flat siblings under one parent
			// each inherit their own closest (last) heading, not the first one.
			let lastH = '';
			for (const c of parent.children) {
				if (c === node) break;
				if (/^H[1-6]$/.test(c.tagName)) {
					const t = c.textContent.trim().replace(/\s+/g, ' ');
					if (t) lastH = t;
				}
			}
			if (lastH) return lastH;
		}
		return '';
	}

	// helpTextFor returns descriptive hint text visible below a form field label.
	function helpTextFor(el) {
		const formEl = el.closest(
			'[data-test-form-element], .artdeco-form-element, .fb-form-element, fieldset'
		);
		if (formEl) {
			const hint = formEl.querySelector(
				'.fb-form-element__help-text, [data-test-form-element-description], ' +
				'.artdeco-form-element__helper-text, [class*="helper-text"], ' +
				'[class*="hint-text"], [class*="help-text"], small'
			);
			if (hint) return hint.textContent.trim().replace(/\s+/g, ' ');
		}
		return '';
	}


	// ── Review-only steps (Work Experience / Education cards), no inputs, skip immediately ──
	// These steps show read-only cards with Edit/Remove buttons. There are no form fields
	// to fill; the bot just needs to click Next/Review.
	if (allInDOM(document, '.jobs-easy-apply-repeatable-groupings__groupings').some(isVisible)) {
		return JSON.stringify([]);
	}
	// LinkedIn final review page (heading + Submit): read-only summaries — skip field scan.
	if (linkedInAtFinalReviewPage()) {
		return JSON.stringify([]);
	}

	const fields = [];
	const includeAllCurrentValues = !!(typeof opts !== 'undefined' && opts && opts.includeAnsweredFields);

	// ── File inputs, scan globally via shadow DOM (LinkedIn hides these in shadow roots) ───
	// Filter to document uploads only (PDF/DOC/TXT), skip image/video/etc.
	// Use isContainerVisible so we don't pick up file inputs from off-screen steps.
	documentUploadInputs(container).forEach((el, i) => {
		function classifyDocumentKind(text) {
			const q = (text || '').toLowerCase();
			if (q.includes('cover')) return 'cover_letter';
			if (q.includes('resume') || q.includes('résumé') || q.includes('cv')) return 'resume';
			return 'unknown';
		}
		const formEl = el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element, fieldset, label');
		const lbl = labelFor(el.id)
			|| (formEl && formEl.querySelector('label, legend, .fb-form-element-label, span[data-test-form-element-label]'))
			|| el.closest('label');
		const slot=isNativeSeekApplyPage()?el.closest('[data-testid="resumeFileInput"],[data-testid="coverLetterFileInput"]'):null;
		const question = slot?(slot.getAttribute('data-testid')==='coverLetterFileInput'?'Cover letter':'Resume'):lbl
			? lbl.textContent.trim().replace(/\s+/g, ' ')
			: (el.getAttribute('aria-label') || el.getAttribute('name') || '');
		const required = el.required || el.getAttribute('aria-required') === 'true';
		const siteSelected=slot&&slot.getAttribute('data-testid')==='resumeFileInput'&&allInDOM(container,'input[type="radio"]').some(r=>r.checked&&/^(document-select-|resume-method)/.test(r.name||'')&&!!linkedInResumeFilename(linkedInRadioText(container,r)));
		fields.push({
			type: 'file', id: el.id, index: i, hasFile: !!(el.files && el.files.length > 0)||!!siteSelected,
			question, documentKind: classifyDocumentKind(question), required,
		});
	});

	// ── Upload-via-button resume slots (LinkedIn native SDUI layout) ─────────
	// LinkedIn's new Easy Apply has no visible <input type="file"> in the DOM.
	// Instead it shows an "Upload resume" button. Since the file input scan above
	// found nothing (ResumeFileSlots=0), we detect the button here.
	// Only runs when no visible file input was already found in this section.
	(function() {
		function classifyDocumentKindBtn(lbl) {
			const q = (lbl || '').toLowerCase();
			if (q.includes('cover')) return 'cover_letter';
			if (q.includes('resume') || q.includes('r\xe9sum\xe9') || q.includes('cv')) return 'resume';
			return 'resume'; // default for upload buttons without qualifier
		}
		allInDOM(container, 'button,[role="button"]').filter(isVisible).forEach(btn => {
			const lbl = ((btn.getAttribute('aria-label') || '') + ' ' + (btn.textContent || '')).trim();
			if (!/\bupload\b/i.test(lbl) || !/\b(resume|cv|r\xe9sum\xe9)\b/i.test(lbl)) return;

			// Find the enclosing form section for this button.
			const resumeSection = resolveLinkedInResumeSection(container, btn);
			const sectionEl = resumeSection ? resumeSection.section
				: btn.closest('[data-test-form-element],.artdeco-form-element,.fb-form-element,fieldset,[role="group"]') || btn.parentElement;

			// Skip if the section already has a visible file input (old layout coexisting
			// with the button — e.g. LinkedIn A/B experiment). The file-input scan above
			// already produced the field; adding a button field would double-count.
			const sectionHasVisibleInput = !!(sectionEl && allInDOM(sectionEl, 'input[type="file"]').some(el => {
				const a = (el.accept || '').toLowerCase();
				return (!a || a.includes('pdf') || a.includes('doc') || a.includes('rtf') || a.includes('txt'))
				    && isContainerVisible(el);
			}));
			if (sectionHasVisibleInput) return;

			// hasFile: "Change resume" / "Replace resume" button nearby means a resume was
			// previously selected (analogous to el.files.length > 0 for hidden inputs).
			const hasFile = !!(sectionEl && allInDOM(sectionEl, 'button,[role="button"]').some(el => {
				const t = ((el.getAttribute('aria-label') || '') + ' ' + (el.textContent || '')).toLowerCase();
				return /\b(change|replace)\b/.test(t) && /\b(resume|cv)\b/.test(t);
			}));

			// Requiredness: read from section semantics — input.required is always false
			// when no file input exists. aria-required on the section is confirmed from
			// the native-dialog-resume.html fixture (aria-required="true" on the wrapper).
			const required = !!(
				(sectionEl && (sectionEl.getAttribute('aria-required') === 'true'
				               || sectionEl.querySelector('[aria-required="true"]')))
				|| /[\*✱]/.test((sectionEl && sectionEl.querySelector('label,legend,[class*="label"]') || {}).textContent || '')
			);

			const docKind = classifyDocumentKindBtn(lbl);

			// Identify the radio group within this section.
			// Use the single unique group name if exactly one exists; empty otherwise
			// to avoid guessing when the section is ambiguous or has no radios yet.
			let resumeGroupName = '';
			if (sectionEl) {
				const sectionRadios = allInDOM(sectionEl, 'input[type="radio"]');
				const groupNames = [...new Set(sectionRadios.map(r => r.name).filter(Boolean))];
				if (groupNames.length === 1) resumeGroupName = groupNames[0];
			}

			fields.push({
				type: 'file', index: -1, hasFile, uploadViaButton: true,
				question: lbl.replace(/\s+/g, ' ').trim() || 'Resume',
				documentKind: docKind, required, resumeGroupName,
			});
		});
	})();

	// ── Radio groups, unanswered ones whose CONTAINER is visible ────────────
	// NOTE: LinkedIn hides the actual <input type="radio"> with CSS (opacity:0 / size:0)
	// and renders custom-styled labels. So we check visibility on the parent container,
	// NOT on the radio input itself.
	function isContainerVisible(el) {
		try {
			// Walk up to a reasonable ancestor and check it instead.
			const grp = el.closest('fieldset, [role="group"], [data-test-form-element], .artdeco-form-element, .fb-form-element')
			            || el.parentElement?.parentElement;
			return grp ? isVisible(grp) : isVisible(el);
		} catch(e) { return false; }
	}

	function groupHasValidationError(grp) {
		if (!grp) return false;
		const described = grp.getAttribute('aria-describedby') || '';
		for (const id of described.split(/\s+/).filter(Boolean)) {
			if (id.indexOf('error-message') === 0) return true;
			const el = document.getElementById(id);
			if (el && (el.textContent || '').replace(/\s+/g, ' ').trim().length > 0) return true;
		}
		return !!(grp.querySelector('[aria-invalid="true"]') || grp.querySelector('input[aria-invalid="true"]'));
	}

	function linkedInRadioOptionLabel(scope, radio) {
		let node = radio.parentElement;
		for (let i = 0; i < 4 && node; i++, node = node.parentElement) {
			const p = node.querySelector(':scope > p, :scope p');
			if (p) {
				const t = (p.textContent || '').replace(/\s+/g, ' ').trim();
				if (t && t.length <= 40 && !t.includes('?')) return t;
			}
		}
		return linkedInRadioText(scope, radio);
	}

	function sduiQuestionForFieldset(fieldset) {
		if (!fieldset) return '';
		let prev = fieldset.previousElementSibling;
		if (prev && prev.tagName === 'P') return prev.textContent.trim();
		const parent = fieldset.parentElement;
		if (parent) {
			const p = [...parent.children].find(c => c.tagName === 'P' && (c.textContent || '').includes('?'));
			if (p) return p.textContent.trim();
		}
		return '';
	}

	const seenRadio = new Set();
	allInDOM(container, 'input[type="radio"]').filter(isContainerVisible).forEach(r => {
		if(isNativeSeekApplyPage()&&/^(document-select-|resume-method|coverLetter-method)/.test(r.name||''))return;
		const groupKey = (r.name || r.id) + '|' + sectionGroupFor(r);
		if (!groupKey || seenRadio.has(groupKey)) return;
		seenRadio.add(groupKey);

		let all;
		if (r.name) {
			// Use JS property comparison, CSS selectors break on URN-style names
			// containing colons and parentheses (LinkedIn's form element names).
			all = allInDOM(container, 'input[type="radio"]').filter(x => x.name === r.name && sectionGroupFor(x) === sectionGroupFor(r));
		} else {
			const parent = r.closest('[data-test-form-element], .artdeco-form-element, fieldset, [role="group"]') || r.parentElement?.parentElement;
			all = parent ? allInDOM(parent, 'input[type="radio"]') : [r];
		}
		// Filter to same-group members whose container is visible.
		all = all.filter(isContainerVisible);
		const grp = r.closest('fieldset, [role="group"], .fb-form-element, [data-test-form-element], .artdeco-form-element')
		            || r.parentElement?.parentElement;
		if (all.some(x => x.checked || x.getAttribute('aria-checked') === 'true')) {
			if (!includeAnsweredRadios && !groupHasValidationError(grp)) return;
		}

		// Walk up to find a <legend> as a direct child of an ancestor.
		// Seek puts the legend as a sibling of the options container (5+ levels up
		// from the input), not inside the individual option wrapper that grp lands on.
		let question = '';
		{
			let node = r.parentElement;
			for (let i = 0; i < 8 && node && node !== document.body; i++, node = node.parentElement) {
				const directLegend = [...(node.children || [])].find(c => c.tagName === 'LEGEND');
				if (directLegend) { question = directLegend.textContent.trim(); break; }
			}
		}
		if (!question) {
			const fallback = grp && grp.querySelector('legend, span[class*="label"], div[class*="label"], label');
			question = fallback ? fallback.textContent.trim() : (r.name || r.id || '');
		}
		if (!question && grp) {
			const fs = grp.closest('fieldset') || (grp.tagName === 'FIELDSET' ? grp : null);
			question = sduiQuestionForFieldset(fs) || question;
		}
		if (!question && all[0]) {
			const aria = (all[0].getAttribute('aria-label') || '').trim();
			if (aria) question = aria;
		}

		const options = all.map(x => {
			return { id: x.id, value: x.value, label: linkedInRadioOptionLabel(container, x) };
		});
		fields.push({ type: 'radio', name: r.name || groupKey, question, options,
		              required: all.some(x => x.required || x.getAttribute('aria-required') === 'true') || (grp && grp.getAttribute('aria-required') === 'true'),
		              group: sectionGroupFor(r), helpText: helpTextFor(r) });
	});

	// ── Checkbox groups (Lever ATS / LinkedIn consent questions) ─────────────
	// Group by shared fieldset. Skip LinkedIn's "follow company" checkbox.
	const seenCb = new Set();
	allInDOM(container, 'input[type="checkbox"]').filter(isContainerVisible).forEach(cb => {
		const associatedLabel=labelFor(cb.id);
		const cbLabel = ((associatedLabel&&associatedLabel.textContent) || (cb.closest('label') && cb.closest('label').textContent) || cb.getAttribute('aria-label') || '').toLowerCase().replace(/\s+/g, ' ').trim();
		// A vacancy-tailored upload must not change the candidate's jobsite default.
		if(isSeekApply && /^make this my default (resume|resumé|résumé|cv)$/.test(cbLabel))return;
		if (cb.id === 'follow-company-checkbox' || (cb.name || '').includes('follow-company')) return;
		if (/follow/.test(cbLabel) && (/compan|hiring|recruiter|poster|posting/.test(cbLabel) || cbLabel.includes('follow '))) return;
		const groupEl = cb.closest('fieldset, [role="group"], [data-test-form-element], .artdeco-form-element, .fb-form-element')
		               || cb.parentElement?.parentElement;
		const groupKey = (groupEl && (groupEl.id || groupEl.getAttribute('data-test-form-element'))) || cb.name || cb.id || '';
		if (groupKey && seenCb.has(groupKey)) return;
		if (groupKey) seenCb.add(groupKey);

		let all;
		if (cb.name) {
			all = allInDOM(container, 'input[type="checkbox"]').filter(x => x.name === cb.name && isContainerVisible(x));
		} else if (groupEl) {
			all = allInDOM(groupEl, 'input[type="checkbox"]').filter(isContainerVisible);
		} else {
			all = [cb];
		}
		if (all.length === 0 || (!includeAllCurrentValues && all.every(x => x.checked))) return;

		let question = '';
		{
			let node = cb.parentElement;
			for (let i = 0; i < 8 && node && node !== document.body; i++, node = node.parentElement) {
				const leg = [...(node.children || [])].find(c => c.tagName === 'LEGEND');
				if (leg) { question = leg.textContent.trim(); break; }
			}
		}
		if (!question && groupEl) {
			const fb = groupEl.querySelector('legend, span[class*="label"], div[class*="label"], label:not(:has(input))');
			question = fb ? fb.textContent.trim() : (cb.name || cb.id || '');
		}

		const options = all.map(x => {
			const lbl = labelFor(x.id) || x.closest('label') || x.parentElement;
			return { id: x.id, value: x.value || x.id,
			         label: (lbl ? lbl.textContent : (x.value || x.id)).replace(/\s+/g, ' ').trim() };
		});
		if(options.length===1&&(!question||question===cb.id||question===cb.name))question=options[0].label;
		if(options.length===1&&/^i currently attend this institution$/i.test(options[0].label.trim()))question=options[0].label;
		fields.push({ type: 'checkbox', name: cb.name || groupKey, question, options, group: sectionGroupFor(cb), helpText: helpTextFor(cb), required: all.some(x => x.required || x.getAttribute('aria-required') === 'true') });
	});

	// Seek review step: custom checkbox widgets (role=checkbox) without native inputs.
	if (isSeekApply) {
		const applyRoot = container.closest("[data-testid='quick-apply-form'], [data-automation='apply-form']") || container;
		allInDOM(applyRoot, '[role="checkbox"]').filter(el => {
			return isContainerVisible(el) && el.getAttribute('aria-checked') !== 'true';
		}).forEach((w, i) => {
			if (!w.id) w.setAttribute('data-seek-cb-id', 'seek-cb-' + i);
			const wid = w.id || w.getAttribute('data-seek-cb-id');
			if (seenCb.has('role:' + wid)) return;
			seenCb.add('role:' + wid);
			const near = w.closest('fieldset, [role="group"], div, label') || w.parentElement;
			const question = (near && near.textContent || w.getAttribute('aria-label') || 'Consent').trim().replace(/\s+/g, ' ');
			fields.push({ type: 'checkbox', name: wid, question,
				options: [{ id: wid, value: 'yes', label: question }] });
		});
	}

	// ── Native <select>, unset / at placeholder ──────────────────────────────
	// Skip selects that share a form-element with a textarea — those labels belong
	// to open-ended questions; the select is often a leftover/hidden control.
	allInDOM(container, 'select').filter(isVisible).forEach((sel, i) => {
		const formEl = sel.closest('[data-test-form-element], .fb-form-element, .artdeco-form-element, fieldset');
		if (formEl && allInDOM(formEl, 'textarea').length > 0) return;
		const blank = !sel.value || sel.selectedIndex <= 0
		              || (sel.options[sel.selectedIndex] && sel.options[sel.selectedIndex].disabled)
		              || sel.getAttribute('aria-invalid') === 'true';
		if (!includeAllCurrentValues && !blank && sel.getAttribute('aria-invalid') !== 'true') return;
		const lbl = labelFor(sel.id)
		            || (formEl && formEl.querySelector('label, legend, .fb-form-element-label, span[aria-hidden="true"]'));
		const opts = [...sel.options]
			.filter(o => !o.disabled && o.value !== '')
			.map(o => ({ value: o.value, label: o.text.trim() }));
		if (opts.length === 0) return;
		if ((!lbl || !lbl.textContent.trim()) && !sel.required && sel.getAttribute('aria-required') !== 'true') return;
		fields.push({ type: 'select', id: sel.id,
		              required: sel.required || sel.getAttribute('aria-required') === 'true',
		              question: lbl ? lbl.textContent.trim().replace(/\s+/g, ' ') : '', options: opts,
		              group: sectionGroupFor(sel), helpText: helpTextFor(sel) });
	});

	// ── Text / number / tel / email / textarea that are empty ─────────────────
	// Track a per-page index so duplicate element IDs (LinkedIn reuses the same ID
	// for every text input) can be disambiguated when filling.
	let textIdx = 0;
	allInDOM(container, 'input, textarea').filter(el => {
		if (el.tagName !== 'TEXTAREA') {
			const ty = (el.getAttribute('type') || 'text').toLowerCase();
			if (['hidden', 'file', 'radio', 'checkbox', 'submit', 'button', 'reset', 'image'].includes(ty)) return false;
		}
		// Exclude reCAPTCHA injected textareas (hidden by LinkedIn/Google on form pages).
		if (el.name === 'g-recaptcha-response' || el.id === 'g-recaptcha-response') return false;
		if (el.closest('[id*="recaptcha" i],[class*="recaptcha" i],[id*="captcha" i],[class*="captcha" i]')) return false;
		// Textareas are often opacity/size-hidden until focused — use container visibility.
		if (el.tagName === 'TEXTAREA') return isContainerVisible(el);
		return isVisible(el);
	}).forEach(el => {
		// Detect typeahead FIRST, combobox fields may already have a typed value
		// but still need dropdown confirmation. Always include them even when non-empty.
		const isTypeahead = el.getAttribute('role') === 'combobox'
		                 || el.getAttribute('aria-autocomplete') === 'list'
		                 || el.getAttribute('aria-autocomplete') === 'both'
		                 || (el.closest('.search-basic-typeahead, [data-test-single-typeahead-entity-form-component]') != null);
		if (!includeAllCurrentValues && el.value.trim() && !isTypeahead && el.getAttribute('aria-invalid') !== 'true') { textIdx++; return; }
		const isRequired = el.required || el.getAttribute('aria-required') === 'true'
		                || el.getAttribute('aria-invalid') === 'true';
		const formEl = el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element, fieldset');
		const lbl = labelFor(el.id)
		    || (function() {
		        const ids = (el.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean);
		        if (!ids.length) return null;
		        const els = ids.map(function(id) { return document.getElementById(id); }).filter(Boolean);
		        const text = els.map(function(e) { return e.textContent; }).join(' ').replace(/\s+/g, ' ').trim();
		        return text ? { textContent: text } : null;
		      })()
		    || (function() {
		        const t = (el.getAttribute('aria-label') || '').trim();
		        return t ? { textContent: t } : null;
		      })()
		    || (formEl && formEl.querySelector('label, legend, .fb-form-element-label, span[data-test-form-element-label]'))
		    || el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element')?.querySelector('label');
		// Seek Quick Apply often omits required/labels on screening inputs — include
		// any empty field inside the apply form container.
		if (!isRequired && !lbl && el.tagName !== 'TEXTAREA' && !isSeekApply) { textIdx++; return; }
		const question = lbl
			? lbl.textContent.trim().replace(/\s+/g, ' ')
			: (el.placeholder || el.getAttribute('aria-label') || el.getAttribute('name') || '');
		if (!question && el.tagName !== 'TEXTAREA') { textIdx++; return; }
		const maxChars = charLimitForField(el);
		fields.push({ type: 'text', id: el.id, index: textIdx,
		              question: question || el.placeholder || 'Additional details',
		              isTypeahead,
		              maxChars, required: isRequired,
		              minValue: el.getAttribute('min') || '', maxValue: el.getAttribute('max') || '',
		              stepValue: el.getAttribute('step') || '', pattern: el.getAttribute('pattern') || '',
		              locale: el.closest('[lang]')?.getAttribute('lang') || document.documentElement.lang || '',
		              nativeType: el.tagName === 'TEXTAREA' ? 'textarea' : el.type,
		              group: sectionGroupFor(el), helpText: helpTextFor(el),
		              inputType: (el.id.endsWith('-numeric') || el.type === 'number' || el.getAttribute('inputmode') === 'numeric')
		                         ? 'numeric'
		                         : (el.tagName === 'TEXTAREA' ? 'textarea' : (el.type || 'text')) });
		textIdx++;
	});

	return JSON.stringify(fields);
`

const jsHasVisibleFormContentInner = `
			function isVisible(el) {
				try {
					const rect = el.getBoundingClientRect();
					if (rect.width === 0 && rect.height === 0) return false;
					if (rect.right <= 0 || rect.left >= window.innerWidth) return false;
					if (rect.bottom <= 0 || rect.top >= window.innerHeight) return false;
					let node = el;
					while (node && node !== document.documentElement) {
						const s = window.getComputedStyle(node);
						if (s.display === 'none' || s.visibility === 'hidden') return false;
						node = node.parentElement;
					}
					return true;
				} catch(e) { return false; }
			}
			if (linkedInAtFinalReviewPage()) return 0;
			const root = resolveApplyContainer();
			if (!root) return 0;
			return [...root.querySelectorAll('input, select, textarea')]
				.filter(el => {
					if (el.tagName === 'INPUT') {
						const ty = (el.getAttribute('type') || 'text').toLowerCase();
						if (['hidden', 'file', 'radio', 'checkbox', 'submit', 'button', 'reset', 'image'].includes(ty)) return false;
					}
					return isVisible(el) && (!el.value.trim() || el.getAttribute('aria-invalid') === 'true');
				}).length;
`

var jsScanFields string
var jsHasVisibleFormContent string
var jsLinkedInClickUploadResume string
var jsLinkedInCheckResumeAccepted string
var jsLinkedInCheckResumeAcceptedLite string
var jsLinkedInSelectResumeRadioByFilename string
var jsLinkedInFinalReviewResumeCheck string
var jsLinkedInFindUploadButton string
var jsLinkedInResumePreUploadState string
var jsLinkedInResumeRadioGroupName string
var jsFindCheckedRadioLabel string

// jsLinkedInApplyContainerFn defines resolveApplyContainer() — the single source of truth
// for locating the active Easy Apply dialog. Uses document.querySelectorAll (no allInDOM
// dependency) so it can be embedded in any JS constant. Priority:
//  1. LinkedIn native <dialog open> — confirmed by data-sdui-screen*="EasyApply" inside
//  2. Legacy LinkedIn class-based modal (.jobs-easy-apply-content, .jobs-easy-apply-modal)
//  3. Seek form selectors
//
// Embedded before jsScanFieldsInner, jsLinkedInAtFinalReviewFunction, and jsClickApplyNavigation
// so resolveApplyContainer() is in scope wherever those functions run.
const jsLinkedInApplyContainerFn = jsGreenhouseFormScopeFn + jsDocumentUploadInputsFn + `
function isNativeSeekApplyPage() {
 return location.protocol==='https:' && /(^|\.)seek\.(com|com\.au|co\.nz)$/.test(location.hostname)
   && /^\/job\/\d{6,}\/apply(?:\/|$)/.test(location.pathname);
}
function resolveApplyContainer() {
	if (/^(boards|job-boards)\.greenhouse\.io$/.test(window.location.hostname)) {
		return resolveGreenhouseApplicationForm();
	}
	function rectVisible(el) {
		try { const r = el.getBoundingClientRect(); return r.width > 2 && r.height > 2; }
		catch(e) { return false; }
	}
	// LinkedIn's new SDUI Easy Apply: <dialog open> without role="dialog" attribute.
	// Identified by the SDUI screen value confirmed in Oct/Sep 2026 captures.
	// Only unambiguous when exactly one such element is visible.
	{
		const easyApplyDialogs = [...document.querySelectorAll('dialog[open]')].filter(
			d => rectVisible(d) && d.querySelector('[data-sdui-screen*="EasyApply"]')
		);
		if (easyApplyDialogs.length === 1) return easyApplyDialogs[0];
		if (easyApplyDialogs.length > 1) return null; // ambiguous
	}
	// Legacy LinkedIn: class-based modal (retained for accounts/regions not yet migrated).
	for (const sel of ['.jobs-easy-apply-content', '.jobs-easy-apply-modal']) {
		const el = document.querySelector(sel);
		if (el && rectVisible(el)) return el;
	}
	// LinkedIn role="dialog" fallback: covers older A/B modal variants where the apply
	// container has role="dialog" but not the SDUI-era class names. Only unambiguous
	// when exactly one such element is visible (multiple visible dialogs = skip).
	{
		const roleDialogs = [...document.querySelectorAll('[role="dialog"]')].filter(rectVisible);
		if (roleDialogs.length === 1) return roleDialogs[0];
	}
	// Seek form selectors.
	for (const sel of [
		"[data-testid='quick-apply-form']",
		"[data-automation='apply-form']",
		"form[data-testid*='applicationForm']",
	]) {
		const el = document.querySelector(sel);
		if (el && rectVisible(el)) return el;
	}
	// Current SEEK uses a full-page application main without a <form>. Require
	// its native apply route, one main landmark and a known native navigation
	// marker; never expand a missing scope to the whole document.
	if (isNativeSeekApplyPage()) {
		const mains=[...document.querySelectorAll('main,[role="main"]')].filter(rectVisible);
		if(mains.length>1)return null;
		const root=mains[0]||document;
		const controls=[...root.querySelectorAll('button[data-testid="continue-button"],button[data-testid="review-submit-application"],[data-automation="review-submit-button"],button[data-testid="review-submit-button"],button[data-testid="submit-application"]')].filter(rectVisible);
		if(controls.length!==1)return null;
		if(mains.length===1)return mains[0];
		// Some native releases use only divs. Bind the unique native action to
		// the unique page heading through their smallest common container.
		let headings=[...document.querySelectorAll('h1')].filter(rectVisible);
		if(!headings.length)headings=[...document.querySelectorAll('h2')].filter(rectVisible);
		if(headings.length!==1)return null;
		for(let p=controls[0].parentElement;p&&p!==document.body&&p!==document.documentElement;p=p.parentElement) {
			if(!p.contains(headings[0]))continue;
			if([...p.querySelectorAll('header,nav,[role="banner"],[role="navigation"]')].some(el=>el.querySelector('input,select,textarea,[contenteditable="true"]')))return null;
			return p;
		}
	}
	return null;
}

function allInDOM(root, selector) {
	const r = [];
	const walk = (node) => {
		if (!node) return;
		try {
			if (node.querySelectorAll) r.push(...node.querySelectorAll(selector));
		} catch (e) {}
		let kids = [];
		try { kids = node.querySelectorAll ? [...node.querySelectorAll('*')] : []; } catch (e) {}
		for (const el of kids) {
		 if (el.shadowRoot) walk(el.shadowRoot);
		 if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
		  try {
		   const d = el.contentDocument;
		   if (d) walk(d);
		  } catch (e) {}
		 }
		}
	};
	walk(root);
	return r;
}

// Resume controls are scoped by their relationship to the upload button, not
// LinkedIn's generated class names or the presence of a fieldset.
function linkedInRadioText(scope, radio) {
 const ids = (radio.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean);
 const doc = scope.ownerDocument || document;
 const labelled = ids.map(id => {
  try { return doc.getElementById(id); } catch (e) { return null; }
 }).filter(Boolean).map(e => e.textContent || '').join(' ').trim();
 if (labelled) return labelled;
 const aria = (radio.getAttribute('aria-label') || '').trim();
 if (aria) return aria;
 const label = [...scope.querySelectorAll('label[for]')].find(e => e.htmlFor === radio.id)
  || radio.closest('label') || radio.parentElement;
 return label ? (label.textContent || '').trim() : (radio.value || '');
}
function linkedInResumeFilename(text) {
 const m = (text || '').trim().match(/([^\s\/\\<>"'\n\r][^\/\\<>"'\n\r]*?\.(pdf|docx|doc|rtf|txt))(?=\s|$|["'<>)\]])/i);
 return m ? m[1].toLowerCase() : null;
}
function linkedInResumeButtonVisible(el) {
 for (let n = el; n; n = n.parentElement) {
  const style = getComputedStyle(n);
  if (style.display === 'none' || style.visibility === 'hidden') return false;
 }
 const rect = el.getBoundingClientRect();
 return rect.width > 0 && rect.height > 0;
}
function linkedInResumeUploadButtons(scope) {
 let candidates = [];
 try { candidates = [...scope.querySelectorAll('button,[role="button"]')]; } catch (e) {}
 if (!candidates.length) {
  candidates = allInDOM(scope, 'button,[role="button"]');
 }
 return candidates.filter(b => {
  const t = (b.getAttribute('aria-label') || '') + ' ' + (b.textContent || '');
  return /\bupload\b/i.test(t) && /\b(resume|cv|résumé)\b/i.test(t)
   && linkedInResumeButtonVisible(b);
 });
}
function linkedInResumeUploadInProgress(container) {
 // SDUI replaces Upload resume with Uploading while the old library entry
 // remains checked. That selection says nothing about the in-flight upload.
 const controls = [...container.querySelectorAll('button,[role="button"]')];
 if (controls.some(button => linkedInResumeButtonVisible(button) &&
  /\buploading\b/i.test((button.getAttribute('aria-label') || '') + ' ' + (button.textContent || '')))) return true;
 const uploads = linkedInResumeUploadButtons(container);
 return uploads.some(button => button.getAttribute('aria-busy') === 'true' ||
  !!button.querySelector('[role="progressbar"],[aria-busy="true"]'));
}
function linkedInResumeAcceptedFallback(container, base) {
 if (!base) return null;
 const radios = linkedInVisibleResumeRadios(container);
 if (radios.length) return null;
 let sawFilename = false;
 const scan = (root) => {
  let nodes = [];
  try { nodes = [...root.querySelectorAll('span, p, [aria-label]')]; } catch (e) { return; }
  for (const el of nodes) {
   const aria = (el.getAttribute('aria-label') || '').trim().toLowerCase();
   if (aria === ('download ' + base) || aria === base || aria.endsWith('/' + base)) {
    sawFilename = true;
    return;
   }
   if (el.children.length === 0) {
    const text = (el.textContent || '').trim().toLowerCase();
    if (text === base) { sawFilename = true; return; }
   }
  }
 };
 scan(container);
 if (!sawFilename) return null;
 for (const r of container.querySelectorAll('input[type="radio"]')) {
  const label = linkedInResumeFilename(linkedInRadioText(container, r));
  if (label !== base) continue;
  if (!(r.checked || r.getAttribute('aria-checked') === 'true')) {
   const lbl = r.id ? container.querySelector('label[for="' + CSS.escape(r.id) + '"]') : null;
   if (lbl) { lbl.click(); } else { r.click(); }
   r.dispatchEvent(new Event('change', { bubbles: true }));
  }
  return { accepted: true, error: false, filenameVisible: true, inList: true };
 }
 return { accepted: false, error: false, filenameVisible: false, inList: true };
}
function resolveLinkedInResumeSection(container, button) {
 const buttons = linkedInResumeUploadButtons(container);
 if (!button) {
  if (buttons.length !== 1) return null;
  button = buttons[0];
 }
 if (!buttons.includes(button)) return null;
 for (let node = button.parentElement; node && node !== container; node = node.parentElement) {
  // Never treat the whole SDUI screen as a resume section; keep walking for upload-only blocks inside it.
  if (node.hasAttribute('data-sdui-screen')) continue;
  const radios = allInDOM(node, 'input[type="radio"]');
  const named = node.matches('fieldset,section,[role="group"]');
  if (!radios.length) {
   if (named) return {section: node, radios: [], groupName: ''};
   continue;
  }
  const names = new Set(radios.map(r => r.name));
  // A generic wrapper must own one complete, filename-labelled group, with
  // exactly one upload button. Unrelated/mixed groups remain unresolved.
  if (names.size !== 1 || !radios[0].name || linkedInResumeUploadButtons(node).length !== 1)
   return null;
  if (!radios.every(r => linkedInResumeFilename(linkedInRadioText(node, r)))) return null;
  const groupName = radios[0].name;
  if (allInDOM(container, 'input[type="radio"]')
   .some(r => r.name === groupName && !node.contains(r))) return null;
  return {section: node, radios, groupName};
 }
 // Upload-only resume step: one upload button and no filename-labelled resume radios yet.
 const resumeRadioCount = allInDOM(container, 'input[type="radio"]').filter(r => {
  try {
   const rect = r.getBoundingClientRect();
   if (rect.width < 1 && rect.height < 1) return false;
  } catch (e) { return false; }
  return linkedInResumeFilename(linkedInRadioText(container, r));
 }).length;
 if (buttons.length === 1 && resumeRadioCount === 0) {
  let section = button.parentElement;
  while (section && section !== container) {
   if (section.matches('fieldset,section,[role="group"],[data-test-form-element],.artdeco-form-element')) break;
   section = section.parentElement;
  }
  if (!section || section === container) section = button.parentElement;
  return { section, radios: [], groupName: '' };
 }
 return null;
}
function linkedInResumeRadiosScoped(container) {
 const out = [];
 const seen = new Set();
 const add = (list) => {
  for (const r of list) {
   if (seen.has(r)) continue;
   seen.add(r);
   out.push(r);
  }
 };
 try { add(container.querySelectorAll('input[type="radio"]')); } catch (e) {}
 const stack = [container];
 while (stack.length) {
  const root = stack.pop();
  let els = [];
  try { els = [...root.querySelectorAll('*')]; } catch (e) { continue; }
  for (const el of els) {
   if (el.shadowRoot) {
    try { add(el.shadowRoot.querySelectorAll('input[type="radio"]')); } catch (e) {}
    stack.push(el.shadowRoot);
   }
  }
 }
 return out;
}
function linkedInVisibleResumeRadios(container) {
 let radios = [];
 try { radios = [...container.querySelectorAll('input[type="radio"]')]; } catch (e) {}
 let hasFilename = radios.some(r => !!linkedInResumeFilename(linkedInRadioText(container, r)));
 // SDUI native dialog: stay in light DOM; deep shadow walks can stall CDP on large trees.
 const inNativeDialog = !!(container.closest && container.closest('dialog'));
 if (!hasFilename && !inNativeDialog) {
  radios = linkedInResumeRadiosScoped(container);
  hasFilename = radios.some(r => !!linkedInResumeFilename(linkedInRadioText(container, r)));
 }
 return radios.filter(r => {
  try {
   const rect = r.getBoundingClientRect();
   if (rect.width < 1 && rect.height < 1) return false;
  } catch (e) { return false; }
  return !!linkedInResumeFilename(linkedInRadioText(container, r));
 });
}
function linkedInResumeAcceptanceState(container, radios, base) {
 const labels = radios.map(r => linkedInResumeFilename(linkedInRadioText(container, r)));
 const inList = labels.some(f => f === base);
 if (labels.filter(filename => filename === base).length > 1) {
  return { accepted: false, error: false, filenameVisible: false, inList: false, ambiguous: true };
 }
 const resolved = resolveLinkedInResumeSection(container);
 if (!resolved) {
  return { accepted: false, error: false, filenameVisible: false, inList: labels.some(f => f === base) };
 }
 const uploadVisible = linkedInResumeUploadButtons(resolved.section)
  .some(b => linkedInResumeButtonVisible(b));
 if (!uploadVisible) {
  return { accepted: false, error: false, filenameVisible: false, inList };
 }
 const checkedRadios = radios.filter(r =>
  resolved.section.contains(r) && (r.checked || r.getAttribute('aria-checked') === 'true'));
 if (checkedRadios.length !== 1) {
  return { accepted: false, error: false, filenameVisible: false, inList };
 }
 const extracted = linkedInResumeFilename(linkedInRadioText(container, checkedRadios[0]));
 const filenameVisible = extracted !== null && extracted === base;
 return { accepted: true, error: false, filenameVisible, inList };
}
`

func init() {
	jsScanFields = `(opts) => {
	const includeAnsweredRadios = !!(opts && opts.includeAnsweredRadios);
` + jsLinkedInApplyContainerFn + jsLinkedInAtFinalReviewFunction + jsScanFieldsInner + `
}`
	jsHasVisibleFormContent = `() => {
` + jsLinkedInApplyContainerFn + jsLinkedInAtFinalReviewFunction + jsHasVisibleFormContentInner + `
}`

	// jsFindCheckedRadioLabel finds the checked radio in the named group, scoped to the
	// verified apply container so same-named groups on background SDUI screens are ignored.
	// Label lookup is also scoped to the container so document-wide label[for] collisions
	// cannot cross-contaminate the result. Returns "" when the container is missing or
	// no radio in the group is checked.
	jsFindCheckedRadioLabel = `(name) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return '';
	const radios = allInDOM(container, 'input[type="radio"]').filter(r => r.name === name);
	const checked = radios.find(r => r.checked);
	if (!checked) return '';
	const text = linkedInRadioText(container, checked);
	return linkedInResumeFilename(text) || text.toLowerCase();
}`

	jsLinkedInClickUploadResume = `() => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return { ok: false, reason: 'apply container not found — upload cannot be scoped' };
	const btns = [...container.querySelectorAll('button,[role="button"]')];
	const btn = btns.find(b => {
		try {
			const r = b.getBoundingClientRect();
			if (r.width < 2 && r.height < 2) return false;
		} catch(e) { return false; }
		const lbl = ((b.getAttribute('aria-label') || '') + ' ' + (b.textContent || '')).trim();
		return /\bupload\b/i.test(lbl) && /\b(resume|cv|r\xe9sum\xe9)\b/i.test(lbl);
	});
	if (!btn) return { ok: false, reason: 'Upload resume button not found in container' };
	try { btn.scrollIntoView({ block: 'nearest', inline: 'nearest' }); } catch(e) {}
	try { btn.focus(); } catch(e) {}
	const view = btn.ownerDocument && btn.ownerDocument.defaultView || window;
	const opts = { bubbles: true, cancelable: true, view };
	for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
		try {
			if (type.startsWith('pointer')) {
				btn.dispatchEvent(new PointerEvent(type, Object.assign({ pointerId: 1, pointerType: 'mouse' }, opts)));
			} else {
				btn.dispatchEvent(new MouseEvent(type, opts));
			}
		} catch(e) {
			try { btn.dispatchEvent(new MouseEvent(type.replace('pointer', 'mouse'), opts)); } catch(e2) {}
		}
	}
	return { ok: true };
}`

	jsLinkedInCheckResumeAccepted = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return { accepted: false, error: false, filenameVisible: false };
	const hasError = !!container.querySelector('[aria-invalid="true"],.artdeco-inline-feedback--error');
	if (hasError) return { accepted: false, error: true, filenameVisible: false };
	if (linkedInResumeUploadInProgress(container)) return { uploading: true, accepted: false, error: false, filenameVisible: false };

	const base = (expectedFilename || '').toLowerCase();

	const resolved = resolveLinkedInResumeSection(container);
	if (!resolved) {
		return { accepted: false, error: false, filenameVisible: false, inList: false };
	}
	let resumeRadios = resolved.radios;
	if (!resumeRadios.length) {
		resumeRadios = allInDOM(resolved.section, 'input[type="radio"]').filter(r =>
			linkedInResumeFilename(linkedInRadioText(resolved.section, r)));
	}
	if (resumeRadios.length) {
		return linkedInResumeAcceptanceState(container, resumeRadios, base);
	}
	const fallback = linkedInResumeAcceptedFallback(container, base);
	if (fallback) return fallback;
	return { accepted: false, error: false, filenameVisible: false, inList: false };
}`

	// Lightweight resume acceptance probe for the post-upload poll loop. Avoids deep
	// shadow walks so overlapping CDP evals cannot wedge or crash the renderer.
	jsLinkedInCheckResumeAcceptedLite = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return { noDialog: true, accepted: false, error: false, filenameVisible: false };
	const base = (expectedFilename || '').toLowerCase();
	for (const el of container.querySelectorAll('.artdeco-inline-feedback--error,[role="alert"],[id^="error-message-"]')) {
		const t = (el.textContent || '').replace(/\s+/g, ' ').trim().toLowerCase();
		if (!t) continue;
		if ((t.includes('fail') || t.includes('unable')) && (t.includes('upload') || t.includes('resume'))) {
			return { error: true, message: t, accepted: false, filenameVisible: false };
		}
	}
	if (container.querySelector('[aria-invalid="true"]')) {
		return { error: true, accepted: false, filenameVisible: false };
	}
	if (linkedInResumeUploadInProgress(container)) return { uploading: true, accepted: false, error: false, filenameVisible: false };
	const resolved = resolveLinkedInResumeSection(container);
	if (!resolved) return { accepted: false, error: false, filenameVisible: false, inList: false };
	const state = linkedInResumeAcceptanceState(container, resolved.radios, base);
	return Object.assign({}, state, { accepted: state.accepted && state.filenameVisible, identityMismatch: state.ambiguous || (state.accepted && !state.filenameVisible) });
}`

	// Post-upload: read the sole resume-library radio group after lazy-column injection.
	jsLinkedInResumeRadioGroupName = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return '';
	const base = (expectedFilename || '').toLowerCase();
	const radios = linkedInVisibleResumeRadios(container);
	if (!radios.length) return '';
	const names = [...new Set(radios.map(r => r.name).filter(Boolean))];
	if (names.length !== 1) return '';
	if (base) {
		const checked = radios.filter(r => r.checked || r.getAttribute('aria-checked') === 'true');
		if (checked.length !== 1) return '';
		const fn = linkedInResumeFilename(linkedInRadioText(container, checked[0]));
		if (fn !== base) return '';
	}
	return names[0];
}`

	jsLinkedInSelectResumeRadioByFilename = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const base = (expectedFilename || '').toLowerCase();
	const container = resolveApplyContainer();
	if (!container || !base) return false;
	if (linkedInResumeUploadInProgress(container)) return false;
	const resolved = resolveLinkedInResumeSection(container);
	if (!resolved) return false;
	const matches = resolved.radios.filter(r => linkedInResumeFilename(linkedInRadioText(resolved.section, r)) === base);
	if (matches.length !== 1) return false;
	for (const r of matches) {
		if (r.checked || r.getAttribute('aria-checked') === 'true') return true;
		const lbl = r.id ? container.querySelector('label[for="' + CSS.escape(r.id) + '"]') : null;
		if (lbl) { lbl.click(); } else { r.click(); }
		r.dispatchEvent(new Event('change', { bubbles: true }));
		r.dispatchEvent(new Event('input', { bubbles: true }));
		return true;
	}
	return false;
}`

	jsLinkedInFinalReviewResumeCheck = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return { verified: false };
	const base = (expectedFilename || '').toLowerCase();
	if (!base) return { verified: true };
	function isVisible(el) {
		try {
			const rect = el.getBoundingClientRect();
			if (rect.width < 2 && rect.height < 2) return false;
			let node = el;
			while (node && node !== document.documentElement) {
				const s = window.getComputedStyle(node);
				if (s.display === 'none' || s.visibility === 'hidden') return false;
				node = node.parentElement;
			}
			return true;
		} catch (e) { return false; }
	}
	function extractFilename(text) {
		const t = (text || '').trim();
		const m = t.match(/([^\s\/\\<>"'\n\r][^\/\\<>"'\n\r]*?\.(pdf|docx|doc|rtf|txt))(?=\s|$|["'<>)\]])/i);
		return m ? m[1].toLowerCase() : null;
	}
	const headingSelector = 'h1,h2,h3,h4,h5,h6,legend,[role="heading"],[class*="heading"]';
	const headings = [...container.querySelectorAll(headingSelector)].filter(isVisible);
	const resumeHeadings = headings.filter(h => /^(resume|résumé|cv)\s*[:*]?$/i.test((h.textContent || '').trim()));
	if (resumeHeadings.length !== 1) return { verified: false };
	const resumeHeading = resumeHeadings[0];
	// SDUI uses ordinary divs for the heading row and attachment card. Walk only
	// within this heading's block; never absorb another section or the LazyColumn.
	for (let section = resumeHeading.parentElement; section && section !== container; section = section.parentElement) {
		if (section.getAttribute('data-component-type') === 'LazyColumn') break;
		if (headings.some(h => h !== resumeHeading && section.contains(h))) break;
		const candidates = new Set();
		// Read individual filename leaves, not concatenated instructions/date text.
		for (const el of section.querySelectorAll('*')) {
			if (!isVisible(el) || resumeHeading.contains(el) || el.closest('button')) continue;
			if (el.children.length === 0) {
				const text = (el.textContent || '').trim();
				const filename = extractFilename(text);
				if (filename && filename === text.toLowerCase()) candidates.add(filename);
			}
			const download = (el.getAttribute('aria-label') || '').match(/^download\s+(.+)$/i);
			if (download) {
				const filename = extractFilename(download[1]);
				if (filename && filename === download[1].trim().toLowerCase()) candidates.add(filename);
			}
		}
		if (candidates.size > 0) {
			const names = [...candidates];
			return { verified: names.length === 1 && names[0] === base,
				observedFilename: names.length === 1 ? names[0] : '', reason: names.length === 1 ? 'attachment_read' : 'ambiguous_attachments' };
		}
	}
	return { verified: false, reason: 'resume_attachment_not_found' };
}`

	jsLinkedInFindUploadButton = `() => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return null;
	const buttons = linkedInResumeUploadButtons(container);
	return buttons.length === 1 ? buttons[0] : null;
}`

	jsLinkedInResumePreUploadState = `(expectedFilename) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	if (!container) return { filenameExisted: false, stateVerified: false, sectionFound: false };

	const base = (expectedFilename || '').toLowerCase();
	const resolved = resolveLinkedInResumeSection(container);
	if (!resolved) return { filenameExisted: false, stateVerified: false, sectionFound: false };
	const radios = resolved.radios;
	if (!radios.length) {
		const section = resolved.section;
		const named = section.matches('fieldset,section,[role="group"]');
		if (named) {
			return { filenameExisted: false, stateVerified: false, sectionFound: true };
		}
		const inNativeDialog = container.matches('dialog') || !!(container.closest && container.closest('dialog'));
		let resumeRadioCount = 0;
		if (inNativeDialog) {
			resumeRadioCount = [...container.querySelectorAll('input[type="radio"]')].filter(r =>
				linkedInResumeFilename(linkedInRadioText(container, r))).length;
		} else {
			resumeRadioCount = allInDOM(container, 'input[type="radio"]').filter(r =>
				linkedInResumeFilename(linkedInRadioText(container, r))).length;
		}
		if (resumeRadioCount === 0) {
			return { filenameExisted: false, stateVerified: true, sectionFound: true };
		}
		return { filenameExisted: false, stateVerified: false, sectionFound: true };
	}
	const filenameExisted = radios.some(r =>
		linkedInResumeFilename(linkedInRadioText(resolved.section, r)) === base);

	return { filenameExisted, stateVerified: true, sectionFound: true };
}`
}

// jsFillRadio clicks a radio button by group name and matching option value/label.
// Uses shadow DOM walking to find elements inside LinkedIn's artdeco modal.
const jsFillRadio = `(name, value, force) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	function isSelected(el) {
		return el.checked || el.getAttribute('aria-checked') === 'true';
	}
	// Find all radios with matching name (by property, not CSS selector, to handle special chars).
	const radios = allInDOM(document, 'input[type="radio"]').filter(r => r.name === name);
	if (!radios.length) return false;
	const lower = value.toLowerCase().trim();
	const target = radios.find(r => {
		if (r.value.toLowerCase().trim() === lower) return true;
		const lbl = allInDOM(document, 'label[for]').find(l => l.htmlFor === r.id)
		            || r.closest('label') || r.parentElement;
		return lbl && lbl.textContent.toLowerCase().includes(lower);
	}) || radios[0]; // fallback to first if no match
	if (isSelected(target) && !force) return true; // already selected, don't toggle unless force
	// Prefer label click — custom radio UIs respond more reliably than input.click().
	const lbl = [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === target.id);
	if (lbl) { lbl.click(); } else { target.click(); }
	target.dispatchEvent(new Event('change', { bubbles: true }));
	target.dispatchEvent(new Event('input',  { bubbles: true }));
	return true;
}`

// jsFillRadioByID clicks a specific radio input by its element ID.
// More reliable than jsFillRadio when names contain special characters or are missing.
const jsFillRadioByID = `(id, force) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	let el = document.getElementById(id);
	if (!el || (el.type || '').toLowerCase() !== 'radio') {
		el = allInDOM(document, 'input[type="radio"]').find(r => r.id === id);
	}
	if (!el) return false;
	if (!force && (el.checked || el.getAttribute('aria-checked') === 'true')) return true;
	// Prefer clicking the associated label — custom radio UIs (Seek) build their
	// visual from label-adjacent elements and respond more reliably to label clicks.
	const lbl = [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === id);
	if (lbl) { lbl.click(); } else { el.click(); }
	el.dispatchEvent(new Event('change', { bubbles: true }));
	el.dispatchEvent(new Event('input',  { bubbles: true }));
	return true;
}`

// jsFindCheckedRadioLabel is built in init() — requires resolveApplyContainer()
// from jsLinkedInApplyContainerFn to scope the lookup to the active apply dialog.

// jsFillSelect sets a native <select> using React-compatible native value setter.
// Uses shadow DOM walking to find the element.
const jsFillSelect = `(id, value) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	const sel = allInDOM(document, 'select').find(s => s.id === id);
	if (!sel) return false;
	try {
		const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set;
		setter.call(sel, value);
	} catch(e) { sel.value = value; }
	sel.dispatchEvent(new Event('change', { bubbles: true }));
	return true;
}`

// jsFillCheckbox checks a single checkbox by element ID, dispatching click+change events.
const jsFillCheckbox = `(id) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	const cb = allInDOM(document, 'input[type="checkbox"]').find(e => e.id === id);
	if (cb) {
		if (!cb.checked) {
			cb.click();
			cb.dispatchEvent(new Event('change', { bubbles: true }));
		}
		return true;
	}
	const w = allInDOM(document, '[role="checkbox"]').find(e => e.id === id || e.getAttribute('data-seek-cb-id') === id);
	if (w) {
		if (w.getAttribute('aria-checked') !== 'true') w.click();
		return true;
	}
	return false;
}`

// jsTypeaheadDebug returns a string describing all potential suggestion elements
// near the active combobox, used when no suggestion is detected, to diagnose
// what's actually in the DOM.
const jsTypeaheadDebug = `() => {
	function visible(el) {
		const r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < window.innerHeight + 200;
	}
	const parts = [];
	// Combobox state
	const cb = document.querySelector('[role="combobox"]');
	if (cb) parts.push('combobox expanded=' + cb.getAttribute('aria-expanded') + ' val=' + JSON.stringify(cb.value.substring(0,20)));
	// Known dropdown selectors
	const sels = ['[role="option"]','[role="listbox"]','.basic-typeahead__selectable',
	              '.search-basic-typeahead li','.search-typeahead-v2__hit',
	              '[data-test-autocomplete-results-container] li'];
	for (const s of sels) {
		const n = document.querySelectorAll(s).length;
		if (n > 0) parts.push(s + '=' + n);
	}
	// Walk up from combobox
	if (cb) {
		let node = cb.parentElement;
		for (let i = 0; i < 6 && node; i++, node = node.parentElement) {
			const items = [...node.querySelectorAll('li, [role="option"]')].filter(visible);
			if (items.length) parts.push('walkup[' + i + '] ' + node.tagName + '.' + (node.className||'').substring(0,30) + ':' + items.length + 'items');
		}
	}
	return parts.join(' | ') || 'nothing found';
}`

// jsTypeaheadSuggestion returns the text of the first visible typeahead suggestion,
// or null if no suggestion list is present.  Called after filling a text field to
// detect LinkedIn's city/location autocomplete dropdown.
const jsTypeaheadSuggestion = `() => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	function visible(el) {
		const r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < window.innerHeight + 200;
	}
	const selectors = [
		'[data-test-autocomplete-results-container] li',
		'.basic-typeahead__selectable',
		'.search-typeahead-v2__hit',
		'[role="listbox"] [role="option"]',
		'[role="option"]',
		'.search-basic-typeahead li',
	];
	for (const sel of selectors) {
		const items = allInDOM(document, sel).filter(visible);
		if (items.length > 0) return items[0].textContent.trim();
	}
	// Strategy 2: walk up from the active combobox and find any visible <li>.
	const cb = document.querySelector('[role="combobox"]');
	if (cb) {
		let node = cb.parentElement;
		for (let i = 0; i < 6 && node; i++, node = node.parentElement) {
			const items = [...node.querySelectorAll('li, [role="option"]')].filter(visible);
			if (items.length > 0) return items[0].textContent.trim();
		}
	}
	return null;
}`

// jsClickTypeaheadFirst clicks the first visible suggestion in a typeahead dropdown.
const jsClickTypeaheadFirst = `() => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	function visible(el) {
		const r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < window.innerHeight + 200;
	}
	const selectors = [
		'[data-test-autocomplete-results-container] li',
		'.basic-typeahead__selectable',
		'.search-typeahead-v2__hit',
		'[role="listbox"] [role="option"]',
		'[role="option"]',
		'.search-basic-typeahead li',
	];
	for (const sel of selectors) {
		const items = allInDOM(document, sel).filter(visible);
		if (items.length > 0) { items[0].click(); return true; }
	}
	const cb = document.querySelector('[role="combobox"]');
	if (cb) {
		let node = cb.parentElement;
		for (let i = 0; i < 6 && node; i++, node = node.parentElement) {
			const items = [...node.querySelectorAll('li, [role="option"]')].filter(visible);
			if (items.length > 0) { items[0].click(); return true; }
		}
	}
	return false;
}`

// jsFocusAndClearInput focuses the nth input with the given id and clears its value
// via the React-compatible native setter.  Called before CDP keyboard dispatch so the
// field is focused and empty before we start typing.
const jsFocusAndClearInput = `(id, nth) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	const all = allInDOM(document, 'input, textarea').filter(e => e.id === id);
	const el = all[nth] || all[0];
	if (!el) return false;
	el.focus();
	el.click();
	try {
		const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
		setter.call(el, '');
		el.dispatchEvent(new Event('input', { bubbles: true }));
	} catch(e) { el.value = ''; }
	return true;
}`

// jsFillText sets a text/textarea using React-compatible native value setter.
// Uses shadow DOM walking to find the element. nthMatch selects among duplicate IDs.
const jsFillText = `(id, nthMatch, value) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
				if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
					try {
						const d = el.contentDocument;
						if (d) r.push(...allInDOM(d, selector));
					} catch (e) {}
				}
			}
		} catch(e) {}
		return r;
	}
	const matches = allInDOM(document, 'input, textarea').filter(i => i.id === id);
	const el = matches[nthMatch] || matches[0];
	if (!el) return false;
	try {
		const proto = el.tagName === 'TEXTAREA'
			? window.HTMLTextAreaElement.prototype
			: window.HTMLInputElement.prototype;
		const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
		setter.call(el, value);
	} catch(e) { el.value = value; }
	el.dispatchEvent(new Event('input',  { bubbles: true }));
	el.dispatchEvent(new Event('change', { bubbles: true }));
	el.dispatchEvent(new Event('blur',   { bubbles: true }));
	return true;
}`

// jsFillFileNth marks a current document input inside the active application.
// Prefer a unique scanned ID; the no-ID ordinal uses exactly the scanner's list.
const jsFillFileNth = `(index, id) => {
` + jsLinkedInApplyContainerFn + `
	allInDOM(document, 'input[data-rod-upload-target]').forEach(el => el.removeAttribute('data-rod-upload-target'));
	const inputs = documentUploadInputs(resolveApplyFormContainer());
	const matches = id ? inputs.filter(el => el.id === id) : inputs.filter((_, i) => i === index);
	if (matches.length !== 1) return false;
	matches[0].setAttribute('data-rod-upload-target', String(index));
	return true;
}`

// ── fillFormStep ──────────────────────────────────────────────────────────────

// FillFormStepOpts adjusts scanning/filling for a single apply step.
type FillFormStepOpts struct {
	// IncludeAnsweredRadios re-scans radio groups that already have a selection
	// (LinkedIn session defaults) and groups flagged with SDUI validation errors.
	IncludeAnsweredRadios bool
	// VisionFallback calls IdentifyFormFields when DOM scan finds nothing but inputs are visible.
	VisionFallback bool
}

func mergeFillFormStepOpts(opts []FillFormStepOpts) FillFormStepOpts {
	out := FillFormStepOpts{VisionFallback: true}
	if len(opts) == 0 {
		return out
	}
	out.IncludeAnsweredRadios = opts[0].IncludeAnsweredRadios
	if !opts[0].VisionFallback {
		out.VisionFallback = false
	}
	return out
}

// maxPrepareApplySteps caps prepare scans; apply flows are dynamic length (not a fixed step count).
const maxPrepareApplySteps = 40

// maxExperienceYears is the upper bound LinkedIn uses on most "years of experience" fields.
const maxExperienceYears = 99

// extractFirstNumber returns the first plausible experience-year value in s.
// Used to clean up verbose LLM answers for numeric form fields (e.g. "14 years" → "14").
// Product/version numbers (e.g. "365" in Dynamics 365) are ignored; returns "0" when none fit.
var reNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)
var reYearsNum = regexp.MustCompile(`(?i)(?:^|\s)(\d+(?:\.\d+)?)\s*\+?\s*years?`)
var reYearsRange = regexp.MustCompile(`(?i)(?:^|\s)(\d+(?:\.\d+)?)\s*(?:to|–|-)\s*(\d+(?:\.\d+)?)\s*years?`)

func extractFirstNumber(s string) string {
	return extractFirstNumberMax(s, maxExperienceYears)
}

func extractFirstNumberMax(s string, max int) string {
	if max <= 0 {
		max = maxExperienceYears
	}
	if m := reYearsRange.FindStringSubmatch(s); len(m) > 1 {
		return clampNumericString(m[1], max)
	}
	if m := reYearsNum.FindStringSubmatch(s); len(m) > 1 {
		return clampNumericString(m[1], max)
	}
	nums := reNumber.FindAllString(s, -1)
	for _, n := range nums {
		if plausibleExperienceYear(n, max) {
			return n
		}
	}
	return "0"
}

func plausibleExperienceYear(n string, max int) bool {
	base := strings.Split(n, ".")[0]
	v, err := strconv.Atoi(base)
	return err == nil && v >= 0 && v <= max
}

func clampNumericString(n string, max int) string {
	if !plausibleExperienceYear(n, max) {
		return "0"
	}
	return n
}

// shouldAdvanceApplyForm reports whether Continue/Submit is allowed after fillFormStep.
// LinkedIn Easy Apply and Seek Quick Apply must not advance after a fill error.
func shouldAdvanceApplyForm(fillErr error) bool {
	return fillErr == nil
}

// An unanswered recruiter question cannot authorize a guessed option.
func resolveFormFieldAnswer(answer string, err error, _ string) (string, error) {
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(answer) != "" {
		return answer, nil
	}
	return "", errAdaptiveQuestionReview
}

// A missing optional answer may stay empty. A joined operational failure must
// still stop the form, even when it also carries the question-review sentinel.
func onlyFormQuestionReview(err error) bool {
	if err == errAdaptiveQuestionReview {
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		for _, cause := range causes {
			if !onlyFormQuestionReview(cause) {
				return false
			}
		}
		return len(causes) > 0
	case interface{ Unwrap() error }:
		return onlyFormQuestionReview(wrapped.Unwrap())
	}
	return false
}

// fillFormStep scans the current form page/modal step for unanswered fields and
// fills them using profile defaults, heuristics, or LLM.
//
// Returns (filled, hasFields, err):
//   - filled=true  when at least one field was successfully written
//   - hasFields=true when the scan found at least one field (even if none could be filled)
//   - err non-nil on fatal LLM failures (Jobifai quota or provider usage limits) — callers must not click Continue/Submit
//
// jsFillByQuestionAnswer finds a visible field by question substring and sets/selects the answer.
// Optional 4th argument sectionHint (string): when non-empty, restricts the search to form
// elements whose nearest enclosing section heading contains the hint. Enables filling two
// fields with identical labels that belong to different form sections.
// Optional 5th argument controlID (string): when non-empty and fieldType is not radio/select,
// targets the element by DOM id directly — bypassing label search and section filtering.
const jsFillByQuestionAnswer = `(questionNeedle, answer, fieldType, sectionHint, controlID) => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer() || document.body;
	const needle = (questionNeedle || '').toLowerCase().trim();
	const ans = (answer || '').trim();
	if (!needle || !ans) return false;
	const hint = (sectionHint || '').toLowerCase().trim();

	function allInDOM(root, sel) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(sel));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, sel));
			}
		} catch (e) {}
		return r;
	}

	// When sectionHint is set, only consider elements whose ancestor heading
	// text contains the hint (case-insensitive). Skips elements not in a matching
	// section so identical labels in different sections are filled independently.
	function inSection(el) {
		if (!hint) return true;
		let node = el;
		for (let d = 0; d < 12 && node && node !== document.body; d++, node = node.parentElement) {
			if (node.getAttribute && node.getAttribute('role') === 'group') {
				const lid = node.getAttribute('aria-labelledby');
				if (lid) {
					const lbl = document.getElementById(lid);
					// A role=group boundary is authoritative: match or exclude.
					if (lbl) return (lbl.textContent || '').toLowerCase().includes(hint);
				}
			}
			const parent = node.parentElement;
			if (!parent) break;
			// Find the LAST heading sibling before this node — that is the section it belongs to.
			// Using first-found would cause fields in later sections to inherit an earlier heading.
			let lastH = null;
			for (const c of parent.children) {
				if (c === node) break;
				if (/^H[1-6]$/.test(c.tagName)) lastH = c;
			}
			if (lastH !== null) {
				// A heading boundary was found — this element belongs to that section only.
				return (lastH.textContent || '').toLowerCase().includes(hint);
			}
		}
		return false;
	}

	// Direct targeting by DOM id: skip label search for text inputs when controlID is known.
	// This handles the case where two fields share an identical label (e.g. "Years of experience"
	// in two separate sections) — the scanned element id routes the fill to the exact control.
	if (fieldType !== 'radio' && fieldType !== 'select') {
		const cid = (controlID || '').trim();
		if (cid) {
			const target = document.getElementById(cid);
			if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA')) {
				const ty = target.tagName === 'INPUT' ? (target.getAttribute('type') || 'text').toLowerCase() : 'textarea';
				if (!['hidden', 'file', 'radio', 'checkbox', 'submit', 'button'].includes(ty)) {
					try {
						const proto = target.tagName === 'TEXTAREA' ?
							window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
						const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
						setter.call(target, ans);
					} catch (e) { target.value = ans; }
					target.dispatchEvent(new Event('input', { bubbles: true }));
					target.dispatchEvent(new Event('change', { bubbles: true }));
					return true;
				}
			}
		}
	}

	if (fieldType === 'radio') {
		const lower = ans.toLowerCase();
		for (const r of allInDOM(container, 'input[type="radio"]')) {
			if (!inSection(r)) continue;
			const aria = (r.getAttribute('aria-label') || '').toLowerCase();
			if (!aria.includes(needle) && needle.length > 10 && !aria.includes(needle.slice(0, 40))) continue;
			const lbl = [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === r.id);
			const opt = (lbl && lbl.textContent || '').toLowerCase();
			const near = r.parentElement && r.parentElement.parentElement;
			const p = near && near.querySelector('p');
			const pTxt = (p && p.textContent || '').toLowerCase().trim();
			if (opt.includes(lower) || pTxt === lower || r.value.toLowerCase() === lower) {
				if (lbl) lbl.click(); else r.click();
				r.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}
		}
		return false;
	}
	if (fieldType === 'select') {
		const lower = ans.toLowerCase();
		// Try direct control-id targeting first; fall back to aria-label search.
		let sel = null;
		const cid = (controlID || '').trim();
		if (cid) {
			const t = document.getElementById(cid);
			if (t && t.tagName === 'SELECT') sel = t;
		}
		if (!sel) {
			for (const s of allInDOM(container, 'select')) {
				if (!inSection(s)) continue;
				const aria = (s.getAttribute('aria-label') || '').toLowerCase();
				if (aria.includes(needle) || (needle.length > 10 && aria.includes(needle.slice(0, 40)))) {
					sel = s;
					break;
				}
			}
		}
		if (!sel) return false;
		for (const opt of sel.options) {
			if ((opt.text || '').toLowerCase() === lower || (opt.value || '').toLowerCase() === lower) {
				try {
					const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set;
					setter.call(sel, opt.value);
				} catch (e) { sel.value = opt.value; }
				sel.dispatchEvent(new Event('input', { bubbles: true }));
				sel.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}
		}
		return false;
	}
	for (const el of allInDOM(container, 'input, textarea')) {
		if (el.tagName === 'INPUT') {
			const ty = (el.getAttribute('type') || 'text').toLowerCase();
			if (['hidden', 'file', 'radio', 'checkbox', 'submit', 'button'].includes(ty)) continue;
		}
		if (!inSection(el)) continue;
		const aria = (el.getAttribute('aria-label') || '').toLowerCase();
		const labelled = aria.includes(needle) || (needle.length > 10 && aria.includes(needle.slice(0, 40)));
		if (!labelled) continue;
		try {
			const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
			const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
			setter.call(el, ans);
		} catch (e) { el.value = ans; }
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	}
	return false;
}`

func (b *Bot) fillFormStep(ctx context.Context, page *rod.Page, lazy *lazyDocGen, opts ...FillFormStepOpts) (filled bool, hasFields bool, err error) {
	fillOpts := mergeFillFormStepOpts(opts)
	// Prefer the document (top page or iframe) that hosts the apply form.
	// LinkedIn SDUI often mounts Easy Apply in a CDP frame opaque to top-page JS.
	pages := []*rod.Page{page}
	if frames, err := page.Elements("iframe, frame"); err == nil {
		for _, fr := range frames {
			if fp, ferr := fr.Frame(); ferr == nil && fp != nil {
				pages = append(pages, fp)
			}
		}
	}

	var fields []formField
	for _, p := range pages {
		_, _ = p.Eval(`() => {
			window.scrollTo(0, 0);
			const f = document.querySelector('form');
			if (f && f !== document.body) f.scrollTop = 0;
		}`)
		time.Sleep(80 * time.Millisecond)

		scanArgs := map[string]interface{}{}
		if fillOpts.IncludeAnsweredRadios {
			scanArgs["includeAnsweredRadios"] = true
		}
		res, err := p.Timeout(10*time.Second).Eval(jsScanFields, scanArgs)
		if err != nil {
			continue
		}
		var scanned []formField
		if err := json.Unmarshal([]byte(res.Value.String()), &scanned); err != nil {
			continue
		}
		if len(scanned) > 0 {
			fields = scanned
			page = p
			break
		}
	}
	if len(fields) == 0 {
		log.Debug().Msg("form: scan found no fields")
		// Probe for visible inputs to distinguish "genuinely empty step" from "selector break".
		hasVisibleButUnscanned := false
		for _, p := range pages {
			if probe, err := p.Timeout(10 * time.Second).Eval(jsHasVisibleFormContent); err == nil {
				if n := probe.Value.Int(); n > 0 {
					log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Int("visible_inputs", n).Msg("form: jsScanFields returned 0 fields but DOM has visible inputs, possible selector break")
					if dir := saveAdaptiveDiagnostic(p, "legacy-selector-break"); dir != "" {
						log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("diagnostic_dir", dir).Msg("form: private application diagnostic saved")
					}
					hasVisibleButUnscanned = true
					break
				}
			}
		}
		if hasVisibleButUnscanned && fillOpts.VisionFallback {
			if vf, vErr := b.fillFormStepFromVision(ctx, page, lazy); vErr != nil {
				return false, true, vErr
			} else if vf {
				return true, true, nil
			}
		}
		if hasVisibleButUnscanned {
			return false, true, fmt.Errorf("%w: visible form controls were not resolved", errAdaptiveQuestionReview)
		}
		return false, false, nil
	}
	// Log each found field so we can debug what's being detected.
	for _, f := range fields {
		log.Debug().Msgf("form: field found type=%s q=%q inputType=%s", f.Type, f.Question, f.InputType)
	}
	log.Info().Int("count", len(fields)).Msg("form: fields to fill")

	// Shadow observation: snapshot the step and run the interpreter in read-only
	// mode so the adaptive engine can record proposals for comparison analysis.
	// Must come before any filling so the snapshot reflects the unmodified page state.
	if b.cfg.ShadowStepFn != nil {
		platform := string(b.cfg.Platform)
		if platform == "" {
			platform = "unknown"
		}
		snap := captureStepSnapshot(fields, nil, platform, 0, "shadow-step")
		scopeJobDependentFields(&snap, lazy.job)
		snap.EmployerKey = lazy.job.Company
		b.cfg.ShadowStepFn(ctx, snap)
	}

	lazy.applyCaps(capsFromFormFields(fields))
	if err := b.prepareLegacyAnswers(ctx, lazy, fields); err != nil {
		return filled, true, err
	}
	defer func() { lazy.answerActiveKey = "" }()
	siteMode := b.policies().ResumeMode == domain.ResumeDocumentModeSiteHosted
	for _, f := range fields {
		if f.Type != "file" {
			continue
		}
		if present, amb := detectSiteResumeOnField(f, siteMode); present {
			lazy.formCaps.SiteResumePresent = true
			if amb {
				lazy.formCaps.SiteResumeAmbiguous = true
			}
		}
	}

	// uploadedResumeGroup is set from f.ResumeGroupName after a via-button upload succeeds.
	// It gates post-upload confirmation to the specific radio group in the upload section;
	// unrelated radio groups that precede or follow the resume section use normal answering.
	var uploadedResumeGroup string
	// uploadedFilename is the bare filename (lowercased) delivered to the upload section.
	// The post-upload confirmation verifies the DOM-checked label matches this exactly.
	var uploadedFilename string

	for _, f := range fields {
		b.shortPause() // human-like pause before each field interaction
		if err := b.ensureAnswerFactsCurrent(lazy); err != nil {
			return filled, true, err
		}
		usedAnswer := ""
		lazy.answerActiveKey = legacyFieldKey(f)
		switch f.Type {

		case "file":
			resumePath, coverPath := lazy.get()
			if lazy.policyBlocked() {
				return filled, true, fmt.Errorf("documents: %s", lazy.holdReason)
			}
			docKind := f.DocumentKind
			if docKind == "" || docKind == "unknown" {
				docKind = documents.KindResume
			}
			path := resumePath
			if docKind == documents.KindCoverLetter {
				path = coverPath
			}
			kindLabel := docKind
			if docKind == documents.KindCoverLetter {
				kindLabel = "cover letter"
			}
			if path != "" && docKind == documents.KindResume && b.cfg.Platform == domain.PlatformLinkedIn &&
				(lazy.reviewPrepareOnly || lazy.frozen) {
				baseName := strings.ToLower(filepath.Base(path))
				if res, evalErr := page.Timeout(3*time.Second).Eval(jsLinkedInCheckResumeAccepted, baseName); evalErr == nil &&
					res.Value.Get("accepted").Bool() && res.Value.Get("filenameVisible").Bool() {
					log.Debug().Str("filename", baseName).Bool("frozen", lazy.frozen).
						Msg("form: resume already selected, skipping re-upload")
					filled = true
					continue
				}
			}
			if path == "" {
				if lazy.useSiteResume && docKind == documents.KindResume {
					if !f.HasFile {
						return filled, true, fmt.Errorf("documents: job-site resume not attached")
					}
					log.Debug().Int("index", f.Index).Msg("form: using verified site-hosted resume")
					filled = true
					continue
				}
				if f.HasFile && b.policies().ResumeMode == domain.ResumeDocumentModeSiteHosted && docKind == documents.KindResume {
					log.Debug().Int("index", f.Index).Msg("form: site resume mode — keeping platform attachment")
					filled = true
					continue
				}
				if f.HasFile && f.Required {
					log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Int("index", f.Index).Str("kind", kindLabel).Msg("form: required upload missing generated file — not substituting platform attachment")
					return filled, true, fmt.Errorf("documents: required %s upload failed", kindLabel)
				}
				if f.HasFile {
					log.Debug().Int("index", f.Index).Str("kind", kindLabel).Msg("form: optional field already has platform attachment")
					continue
				}
				log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Int("index", f.Index).Str("platform", string(b.cfg.Platform)).
					Msg("form: no PDF path for optional file field")
				continue
			}
			if lazy.reviewPrepareOnly {
				b.log.Info().Str("filename", filepath.Base(path)).Bool("via_button", f.UploadViaButton).
					Msg("prepare: file upload started")
			}
			var uploadErr error
			if b.cfg.Platform == domain.PlatformSeek {
				switch docKind {
				case documents.KindCoverLetter:
					uploadErr = b.seekUploadCoverLetter(page, path)
				default:
					uploadErr = b.seekUploadResume(page, path)
				}
			} else if f.UploadViaButton {
				uploadErr = b.formUploadViaButton(ctx, page, path)
			} else {
				uploadErr = b.formUploadFile(page, path, f.Index, f.ID)
			}
			if errors.Is(uploadErr, errUploadIdentityUnverifiable) {
				lazy.holdReason = uploadErr.Error()
				var h *uploadIdentityHold
				if errors.As(uploadErr, &h) {
					lazy.holdStage = h.stage
					lazy.holdFile = h.filename
				}
				if lazy.reviewPrepareOnly {
					filled = true
					return filled, true, uploadErr
				}
				return filled, true, uploadErr
			}
			if uploadErr != nil {
				// Record a non-empty hold stage for a click-sequence failure so the persisted pack
				// shows the real failure point (e.g. upload_click_interactable), not an empty stage.
				var ce *uploadClickError
				if errors.As(uploadErr, &ce) {
					lazy.holdStage = "upload_click_" + ce.stage
					lazy.holdFile = strings.ToLower(filepath.Base(path))
				}
				return filled, true, fmt.Errorf("documents: %s upload failed: %w", kindLabel, uploadErr)
			}
			if lazy.reviewPrepareOnly {
				b.log.Info().Str("filename", filepath.Base(path)).Bool("via_button", f.UploadViaButton).
					Msg("prepare: file upload completed")
			}
			log.Info().Str("path", path).Str("kind", kindLabel).Str("filename", filepath.Base(path)).Msg("form: file uploaded")
			filled = true
			if f.UploadViaButton {
				want := strings.ToLower(filepath.Base(path))
				group := f.ResumeGroupName
				if group == "" {
					if res, evalErr := page.Timeout(3*time.Second).Eval(jsLinkedInResumeRadioGroupName, want); evalErr == nil {
						group = res.Value.String()
					}
				}
				if group != "" {
					uploadedResumeGroup = group
					uploadedFilename = want
					log.Info().Str("group", group).Str("filename", want).Msg("form: resume upload group resolved post-upload")
				} else if lazy.frozen {
					if res, evalErr := page.Timeout(3*time.Second).Eval(jsLinkedInCheckResumeAccepted, want); evalErr == nil &&
						res.Value.Get("filenameVisible").Bool() {
						log.Info().Str("filename", want).Msg("form: frozen approve — resume verified without pre-scan group")
						uploadedFilename = want
					} else {
						lazy.holdReason = errUploadIdentityUnverifiable.Error()
						lazy.holdStage = uploadStageNoResumeGroup
						lazy.holdFile = want
					}
				} else {
					lazy.holdReason = errUploadIdentityUnverifiable.Error()
					lazy.holdStage = uploadStageNoResumeGroup
					lazy.holdFile = want
				}
			}

		case "radio":
			if b.cfg.Platform == domain.PlatformLinkedIn && f.Name != uploadedResumeGroup {
				if resumePath, _ := lazy.peek(); resumePath != "" && linkedInResumeLibraryRadioField(f) {
					want := strings.ToLower(filepath.Base(resumePath))
					if ok := b.fillLinkedInResumeLibraryRadio(page, f, want, fillOpts.IncludeAnsweredRadios); ok {
						log.Info().Str("filename", want).Str("group", f.Name).Msg("form: resume library radio selected")
						filled = true
						continue
					}
					if lazy.frozen {
						if res, evalErr := page.Timeout(3*time.Second).Eval(jsLinkedInCheckResumeAccepted, want); evalErr == nil &&
							res.Value.Get("accepted").Bool() {
							log.Info().Str("filename", want).Msg("form: frozen submit — resume already accepted in library")
							filled = true
							continue
						}
					}
					return filled, true, fmt.Errorf("documents: could not select prepared resume %q in library", want)
				}
			}
			// After a via-button upload, confirm the resume group's post-upload selection
			// directly from the DOM. The upload handler leaves the new entry checked; we
			// verify its label matches the intended filename rather than re-clicking, so
			// neither library entries nor the confirmed entry are disturbed.
			// Unrelated radio groups (f.Name != uploadedResumeGroup) always go through normal answering.
			if f.Name == uploadedResumeGroup {
				uploadedResumeGroup = "" // consume — applies to this group only
				if r2, evalErr2 := page.Timeout(3*time.Second).Eval(jsFindCheckedRadioLabel, f.Name); evalErr2 == nil {
					if checkedLabel := r2.Value.String(); strings.EqualFold(checkedLabel, uploadedFilename) {
						log.Info().Str("question", f.Question).Str("answer", checkedLabel).Msg("form: resume radio confirmed post-upload selection")
						filled = true
						continue
					} else {
						log.Warn().Str("expected", uploadedFilename).Str("got", checkedLabel).Msg("form: resume radio post-upload label mismatch — identity hold")
					}
				}
				// No checked radio or label does not match the uploaded filename — hold.
				lazy.holdReason = errUploadIdentityUnverifiable.Error()
				lazy.holdStage = uploadStagePostUploadMismatch
				lazy.holdFile = uploadedFilename
				filled = true
				continue
			}
			raw, qErr := b.answerFormQuestion(ctx, lazy, f.Question, f.optionLabels())
			if raw == "" && qErr == nil && !f.Required {
				continue
			}
			answer, qErr := resolveFormFieldAnswer(raw, qErr, "")
			if qErr != nil {
				return filled, true, qErr
			}
			if answer == "" {
				continue
			}
			// Prefer filling by element ID (avoids name/attr mismatch in shadow DOM).
			optionID := ""
			for _, opt := range f.Options {
				if strings.EqualFold(opt.Label, answer) || strings.EqualFold(opt.Value, answer) {
					optionID = opt.ID
					break
				}
			}
			radioOK := false
			forceRadio := fillOpts.IncludeAnsweredRadios
			if optionID != "" {
				if r2, err := page.Timeout(12*time.Second).Eval(jsFillRadioByID, optionID, forceRadio); err == nil && r2.Value.Bool() {
					radioOK = true
				}
			}
			if !radioOK {
				// Fallback: fill by group name.
				if r2, err := page.Timeout(12*time.Second).Eval(jsFillRadio, f.Name, answer, forceRadio); err == nil && r2.Value.Bool() {
					radioOK = true
				}
			}
			if !radioOK {
				return filled, true, fmt.Errorf("%w: radio selection failed", errAdaptiveQuestionReview)
			}
			log.Info().Str("question", f.Question).Msg("form: radio answered")
			usedAnswer = answer
			filled = true

		case "checkbox":
			raw, qErr := b.answerFormQuestion(ctx, lazy, f.Question, f.optionLabels())
			if raw == "" && qErr == nil && !f.Required {
				continue
			}
			answer, qErr := resolveFormFieldAnswer(raw, qErr, "")
			if qErr != nil {
				return filled, true, qErr
			}
			var targetID string
			for _, opt := range f.Options {
				if strings.EqualFold(strings.TrimSpace(opt.Label), strings.TrimSpace(answer)) ||
					strings.EqualFold(strings.TrimSpace(opt.Value), strings.TrimSpace(answer)) {
					targetID = opt.ID
					break
				}
			}
			if targetID == "" {
				return filled, true, fmt.Errorf("%w: checkbox answer has no matching control", errAdaptiveQuestionReview)
			}
			if result, err := page.Eval(jsFillCheckbox, targetID); err != nil || result == nil || !result.Value.Bool() {
				return filled, true, fmt.Errorf("%w: checkbox selection failed", errAdaptiveQuestionReview)
			}
			log.Info().Str("question", f.Question).Msg("form: checkbox answered")
			usedAnswer = answer
			filled = true

		case "select":
			labels := f.optionLabels()
			// Open-ended prompts misclassified as <select>: ask LLM for free text,
			// then fill the sibling textarea — never force an essay into a dropdown.
			if looksLikeOpenEndedQuestion(f.Question) {
				answer, qErr := b.answerFormQuestion(ctx, lazy, f.Question, nil)
				if qErr != nil {
					return filled, true, qErr
				}
				if ok := b.fillOpenEndedNearSelect(page, f, answer); ok {
					usedAnswer = answer
					filled = true
					break
				}
				log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Str("question", f.Question).Msg("form: open-ended question has no textarea to fill")
			}
			raw, qErr := b.answerFormQuestion(ctx, lazy, f.Question, labels)
			if raw == "" && qErr == nil && !f.Required {
				continue
			}
			answer, qErr := resolveFormFieldAnswer(raw, qErr, "")
			if qErr != nil {
				return filled, true, qErr
			}
			if answer != "" {
				if chosen, ok := matchSelectOption(f, answer); ok {
					if result, err := page.Eval(jsFillSelect, f.ID, chosen); err != nil || result == nil || !result.Value.Bool() {
						return filled, true, fmt.Errorf("%w: select assignment failed", errAdaptiveQuestionReview)
					}
					log.Info().Str("question", f.Question).Msg("form: select filled")
					usedAnswer = answer
					filled = true
				} else if ok := b.fillOpenEndedNearSelect(page, f, answer); ok {
					usedAnswer = answer
					// LLM returned prose that doesn't match dropdown options — try textarea.
					filled = true
				} else {
					return filled, true, fmt.Errorf("%w: select answer has no matching control", errAdaptiveQuestionReview)
				}
			}

		case "text":
			if ctxErr := ctx.Err(); ctxErr != nil {
				return filled, true, ctxErr
			}
			answer := ""
			var answerErr error
			if planned, ok := preparedLegacyAnswer(lazy, f.Question, f.optionLabels()); ok {
				answer = planned
			} else if lazy.answerPlan != nil {
				answerErr = errAdaptiveQuestionReview
			} else if isSalaryFormQuestion(f.Question) {
				answer, answerErr = b.answerSalaryFormField(ctx, lazy, f)
			} else if profile := b.currentProfile(); profile != nil {
				answer = profileContactAnswer(f.Question, f.InputType, &profile.PersonalInformation)
			}
			if answerErr == nil && answer == "" {
				answer, answerErr = b.answerFormQuestionWithLimit(ctx, lazy, f.Question, nil, f.MaxChars)
			} else if answerErr == nil && f.MaxChars > 0 && !isSalaryFormQuestion(f.Question) {
				answer = truncateRunes(answer, f.MaxChars)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return filled, true, ctxErr
			}
			if answerErr != nil {
				if !f.Required && onlyFormQuestionReview(answerErr) {
					continue
				}
				return filled, true, answerErr
			}
			if strings.TrimSpace(answer) == "" {
				if !f.Required {
					continue
				}
				return filled, true, errAdaptiveQuestionReview
			}
			answer = sanitizeContactFieldAnswer(answer, f.Question, f.InputType)
			if isSalaryFormQuestion(f.Question) {
				answer = sanitizeSalaryFieldAnswer(answer, f.MaxChars)
			} else if f.MaxChars > 0 {
				answer = truncateRunes(answer, f.MaxChars)
			}
			if f.InputType == "numeric" && !isSalaryFormQuestion(f.Question) {
				answer = sanitizeNumericFieldAnswer(answer, f.Question)
			}

			if f.Typeahead {
				if b.cfg.ApplicationAnswers != nil && lazy.answerPlan != nil {
					resolution, ok := lazy.answerPlan[legacyFieldKey(f)]
					field, found := lazy.answerSnapshot.FieldByRef(resolution.Ref)
					if !ok || !found {
						return filled, true, errAdaptiveQuestionReview
					}
					result := fillAdaptiveControl(ctx, page, field, answer, b.currentProfile())
					if result.Err != nil {
						return filled, true, result.Err
					}
					if !result.Filled {
						return filled, true, errAdaptiveQuestionReview
					}
					if err := b.markLegacyAnswerFilled(ctx, lazy, f, answer); err != nil {
						return filled, true, err
					}
					filled = true
					continue
				}
				// LinkedIn city/location combobox fields require isTrusted=true keyboard
				// events to trigger the autocomplete API.  JS-dispatched events have
				// isTrusted=false and are ignored by LinkedIn.  We use CDP
				// Input.dispatchKeyEvent which the browser treats as real user input.
				_, _ = page.Eval(jsFocusAndClearInput, f.ID, f.Index)
				time.Sleep(100 * time.Millisecond)
				for _, char := range answer {
					ch := string(char)
					vk := int(char)
					if char >= 'a' && char <= 'z' {
						vk = int(char - 32) // virtual key code uses the uppercase key
					}
					// keyDown: fires keydown event, NO Text (would cause double insertion)
					_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: ch, NativeVirtualKeyCode: vk, WindowsVirtualKeyCode: vk}.Call(page)
					// char: fires keypress + inserts the character (isTrusted=true)
					_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeChar, Key: ch, Text: ch, UnmodifiedText: ch, NativeVirtualKeyCode: vk, WindowsVirtualKeyCode: vk}.Call(page)
					// keyUp: fires keyup event, NO Text
					_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyUp, Key: ch, NativeVirtualKeyCode: vk, WindowsVirtualKeyCode: vk}.Call(page)
					time.Sleep(50 * time.Millisecond)
				}
				log.Info().Str("question", f.Question).Msg("form: typeahead typed via CDP")

				// Poll for the suggestion dropdown to appear.
				// LinkedIn does NOT reliably set aria-expanded="true" on the combobox,
				// so we detect by presence of suggestion items directly (.basic-typeahead__selectable).
				// Once found, click the first item via JS (most reliable for LinkedIn's dropdowns).
				time.Sleep(600 * time.Millisecond)
				deadline := time.Now().Add(12 * time.Second)
				confirmed := false
				for time.Now().Before(deadline) {
					res, err2 := page.Eval(jsTypeaheadSuggestion)
					if err2 == nil && !res.Value.Nil() && res.Value.String() != "" {
						suggestion := res.Value.String()
						r2, err3 := page.Eval(jsClickTypeaheadFirst)
						if err3 == nil && r2.Value.Bool() {
							log.Info().Str("question", f.Question).Str("suggestion", suggestion).Msg("form: typeahead confirmed via JS click")
							confirmed = true
						} else {
							// JS click failed, fall back to ArrowDown+Enter
							time.Sleep(100 * time.Millisecond)
							_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: "ArrowDown", Code: "ArrowDown", WindowsVirtualKeyCode: 40, NativeVirtualKeyCode: 40}.Call(page)
							time.Sleep(100 * time.Millisecond)
							_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyUp, Key: "ArrowDown", Code: "ArrowDown", WindowsVirtualKeyCode: 40, NativeVirtualKeyCode: 40}.Call(page)
							time.Sleep(300 * time.Millisecond)
							_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13, NativeVirtualKeyCode: 13}.Call(page)
							time.Sleep(100 * time.Millisecond)
							_ = proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyUp, Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13, NativeVirtualKeyCode: 13}.Call(page)
							log.Info().Str("question", f.Question).Msg("form: typeahead confirmed via ArrowDown+Enter fallback")
							confirmed = true
						}
						break
					}
					time.Sleep(400 * time.Millisecond)
				}
				if !confirmed {
					if dbg, err := page.Eval(jsTypeaheadDebug); err == nil {
						log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Str("dom", dbg.Value.String()).Str("question", f.Question).Msg("form: typeahead dropdown not detected")
					} else {
						log.Warn().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", lazy.job.URL).Str("question", f.Question).Msg("form: typeahead dropdown not detected")
					}
				}
				if confirmed {
					filled = true
					if err := b.markLegacyAnswerFilled(ctx, lazy, f, answer); err != nil {
						return filled, true, err
					}
				}
				continue
			}

			// Plain text field, React value setter is fine.
			if result, err := page.Eval(jsFillText, f.ID, f.Index, answer); err != nil || result == nil || !result.Value.Bool() {
				return filled, true, fmt.Errorf("%w: text assignment failed", errAdaptiveQuestionReview)
			}
			if isSalaryFormQuestion(f.Question) && !verifySalaryAssignment(page, f.ID, answer) {
				return filled, true, fmt.Errorf("%w: salary was not accepted intact", errAdaptiveQuestionReview)
			}
			log.Info().Str("question", f.Question).Str("inputType", f.InputType).Msg("form: text filled")
			usedAnswer = answer
			filled = true
		}
		if err := b.markLegacyAnswerFilled(ctx, lazy, f, usedAnswer); err != nil {
			return filled, true, err
		}
	}

	if refilled, rErr := b.refillLinkedInOverLimitFields(ctx, page, lazy); rErr != nil {
		return filled, true, rErr
	} else if refilled {
		filled = true
	}

	if filled {
		time.Sleep(600 * time.Millisecond) // let React process field changes
	}
	return filled, true, nil
}

// formUploadFile uses a unique scanned ID or the current document-input ordinal
// inside the active application, including shadow DOM.
func (b *Bot) formUploadFile(page *rod.Page, filePath string, index int, scannedIDs ...string) error {
	id := ""
	if len(scannedIDs) > 0 {
		id = scannedIDs[0]
	}
	// Mark the target file input via JS (handles shadow DOM).
	res, err := page.Eval(jsFillFileNth, index, id)
	if err != nil || res == nil || !res.Value.Bool() {
		return fmt.Errorf("file input at index %d not found in shadow DOM", index)
	}
	// Now find it via the marker attribute rod can locate.
	el, err := page.ElementByJS(rod.Eval(`() => {` + jsLinkedInApplyContainerFn + `
		const matches = documentUploadInputs(resolveApplyFormContainer()).filter(el => el.hasAttribute('data-rod-upload-target'));
		return matches.length === 1 ? matches[0] : null;
	}`))
	if err != nil {
		return fmt.Errorf("locate marked file input: %w", err)
	}
	if err := el.SetFiles([]string{filePath}); err != nil {
		return fmt.Errorf("set files: %w", err)
	}
	// Clean up marker (walk shadow DOM so it finds it even in shadow roots).
	_, _ = page.Eval(`() => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try {
							const d = el.contentDocument;
							if (d) r.push(...allInDOM(d, selector));
						} catch (e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		allInDOM(document, 'input[data-rod-upload-target]').forEach(el => el.removeAttribute('data-rod-upload-target'));
	}`)
	return nil
}

// holdUpload logs the upload-identity hold through the bot run logger (so it reaches the
// registered log sink) and returns a typed error carrying the stage + filename.
func (b *Bot) holdUpload(stage, filename string) error {
	b.log.Warn().Str("filename", filename).Str("stage", stage).Msg("linkedin: upload identity unverifiable")
	return &uploadIdentityHold{stage: stage, filename: filename}
}

// jsUploadButtonDiag returns SAFE diagnostics for the resolved upload button: geometry,
// computed visibility/pointer-events, disabled state, scroll-ancestor chain, and the element
// hit-tested at the button center (tag/role/class/bounds only). No text content, no values,
// no URLs — safe to persist in logs.
const jsUploadButtonDiag = `() => {
` + jsLinkedInApplyContainerFn + `
	const container = resolveApplyContainer();
	const btns = container ? linkedInResumeUploadButtons(container) : [];
	const btn = btns.length === 1 ? btns[0] : null;
	if (!btn) return JSON.stringify({ found: false, buttonCount: btns.length });
	function desc(el){ if(!el) return null; const r=el.getBoundingClientRect(); return {
		tag: el.tagName, role: el.getAttribute && el.getAttribute("role") || "",
		cls: (typeof el.className==="string"?el.className:"").slice(0,120),
		x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; }
	const r = btn.getBoundingClientRect();
	const cs = getComputedStyle(btn);
	const cx = Math.round(r.x + r.width/2), cy = Math.round(r.y + r.height/2);
	const hit = document.elementFromPoint(cx, cy);
	const hitIsButtonOrDesc = !!(hit && (hit === btn || btn.contains(hit) || (hit.contains && hit.contains(btn))));
	const scrollAncestors = [];
	let n = btn.parentElement;
	while (n && scrollAncestors.length < 6) {
		const s = getComputedStyle(n);
		if (/(auto|scroll)/.test(s.overflowY + s.overflow)) scrollAncestors.push({ tag:n.tagName, cls:(typeof n.className==="string"?n.className:"").slice(0,80), scrollTop:n.scrollTop, clientHeight:n.clientHeight, scrollHeight:n.scrollHeight });
		n = n.parentElement;
	}
	const vh = window.innerHeight, vw = window.innerWidth;
	return JSON.stringify({ found: true,
		rect: { x:Math.round(r.x), y:Math.round(r.y), w:Math.round(r.width), h:Math.round(r.height) },
		inViewport: r.top >= 0 && r.left >= 0 && r.bottom <= vh && r.right <= vw,
		visibility: cs.visibility, display: cs.display, opacity: cs.opacity, pointerEvents: cs.pointerEvents,
		disabled: !!btn.disabled, ariaDisabled: btn.getAttribute && btn.getAttribute("aria-disabled") || "",
		hitPoint: { x:cx, y:cy }, hitElement: desc(hit), hitIsButtonOrDescendant: hitIsButtonOrDesc,
		hitIsButton: !!(hit && (hit === btn || btn.contains(hit))),
		scrollAncestors });
}`

// instrumentedUploadClick performs bounded click sub-stages (scroll, interactability+
// move, enabled, mouse-click) individually, logging each stage's outcome and elapsed time so
// the failing stage is visible in bot_run_logs. Each stage gets
// a fresh child of the live preparation ctx. On failure the returned error names the stage.
func (b *Bot) instrumentedUploadClick(ctx context.Context, page *rod.Page, btn *rod.Element, filename string) error {
	// Safe diagnostics: geometry, clipping ancestors, computed visibility/pointer-events, and the
	// element hit-tested at the button center (tag/role/bounds only — no content). Logged per phase.
	diag := func(phase string) {
		dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if res, derr := page.Context(dctx).Eval(jsUploadButtonDiag); derr == nil {
			b.log.Info().Str("filename", filename).Str("click_stage", phase).
				Str("button_diag", res.Value.String()).Msg("prepare: upload button diagnostics")
		}
	}
	diag("pre_scroll")

	// Scroll stage: center the button in its scroll container (LinkedIn renders the Upload button at
	// the bottom of a clipped scroll area). CDP scrolling does not depend on
	// animation frames, which browsers can suspend for background pages.
	scrollCtx, scrollCancel := context.WithTimeout(ctx, 5*time.Second)
	t0 := time.Now()
	_, scrollErr := btn.Context(scrollCtx).Eval(`() => { this.scrollIntoView({block:'center', inline:'center'}); return true; }`)
	if scrollErr == nil {
		scrollErr = proto.DOMScrollIntoViewIfNeeded{ObjectID: btn.Object.ObjectID}.Call(page.Context(scrollCtx))
	}
	scrollCancel()
	b.log.Info().Str("filename", filename).Str("click_stage", "scroll").Dur("elapsed_ms", time.Since(t0)).Err(scrollErr).Msg("prepare: upload click stage")
	diag("post_center")

	// Bounded interactability retry that PRESERVES the centered position: re-center, then use Rod's
	// Interactable() hit-test (which returns the clickable point only when the button is the topmost
	// element there). On a confirmed hit-test + enabled button, click via a trusted CDP mouse
	// move+click at that exact point. Never click without a confirmed hit-test; never blind-click.
	deadline := time.Now().Add(6 * time.Second)
	attempt := 0
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		attempt++
		rcCtx, rcCancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = btn.Context(rcCtx).Eval(`() => { this.scrollIntoView({block:'center', inline:'center'}); return true; }`)
		rcCancel()
		diag(fmt.Sprintf("retry_%d", attempt))

		enCtx, enCancel := context.WithTimeout(ctx, 1*time.Second)
		enErr := btn.Context(enCtx).WaitEnabled()
		enCancel()
		if enErr != nil {
			lastErr = fmt.Errorf("not enabled: %w", enErr)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		iCtx, iCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		pt, iErr := btn.Context(iCtx).Interactable()
		iCancel()
		if iErr != nil || pt == nil {
			lastErr = fmt.Errorf("hit-test not interactable: %w", iErr)
			if iErr != nil && strings.Contains(iErr.Error(), "CSSPseudoElement") {
				if gErr := b.tryGeometryUploadClick(ctx, page, btn, filename, "pseudo_retry"); gErr == nil {
					return nil
				}
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		cctx, ccancel := context.WithTimeout(ctx, 3*time.Second)
		mErr := moveBoundedMouse(cctx, page, *pt)
		var clkErr error
		if mErr == nil {
			clkErr = clickBoundedMouse(cctx, page, *pt)
		}
		ccancel()
		if mErr == nil && clkErr == nil {
			b.log.Info().Str("filename", filename).Str("click_stage", "mouseclick").Int("attempt", attempt).Msg("prepare: upload click stage")
			diag("post_click")
			return nil
		}
		if clkErr != nil {
			lastErr = clkErr
		} else {
			lastErr = mErr
		}
		time.Sleep(300 * time.Millisecond)
	}
	diag("on_timeout")
	if lastErr != nil && (strings.Contains(lastErr.Error(), "CSSPseudoElement") || strings.Contains(lastErr.Error(), "not interactable")) {
		if gErr := b.tryGeometryUploadClick(ctx, page, btn, filename, "geometry_fallback"); gErr == nil {
			return nil
		}
	}
	b.log.Warn().Str("filename", filename).Str("click_stage", "interactable").Int("attempts", attempt).Err(lastErr).Msg("prepare: upload click stage")
	return &uploadClickError{stage: "interactable", err: lastErr}
}

// tryGeometryUploadClick uses CDP mouse events at a DOM-verified point inside the upload
// button when Rod's Interactable() rejects CSS pseudo-element overlays (common on SDUI).
func (b *Bot) tryGeometryUploadClick(ctx context.Context, page *rod.Page, btn *rod.Element, filename, stage string) error {
	res, err := btn.Context(ctx).Eval(`() => {
		const btn = this;
		const r = btn.getBoundingClientRect();
		if (r.width < 2 || r.height < 2) return { ok: false, reason: 'zero_size' };
		const fracs = [[0.5, 0.5], [0.5, 0.35], [0.5, 0.65], [0.35, 0.5], [0.65, 0.5]];
		for (const [fx, fy] of fracs) {
			const x = r.left + r.width * fx;
			const y = r.top + r.height * fy;
			const hit = document.elementFromPoint(x, y);
			if (hit && (hit === btn || btn.contains(hit))) {
				return { ok: true, x, y };
			}
		}
		return { ok: false, reason: 'covered' };
	}`)
	if err != nil {
		return err
	}
	if !res.Value.Get("ok").Bool() {
		return fmt.Errorf("geometry click: %s", res.Value.Get("reason").String())
	}
	x := res.Value.Get("x").Num()
	y := res.Value.Get("y").Num()
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := page.Context(cctx).Mouse.MoveTo(proto.Point{X: x, Y: y}); err != nil {
		return err
	}
	if err := page.Context(cctx).Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}
	b.log.Info().Str("filename", filename).Str("click_stage", stage).
		Float64("click_x", x).Float64("click_y", y).
		Bool("blind_center", res.Value.Get("blind").Bool()).
		Msg("prepare: upload geometry click")
	return nil
}

// uploadClickError marks a failure during the trusted upload-button click sequence, carrying the
// stable sub-stage (e.g. "interactable") so the hold can be persisted with a non-empty stage.
type uploadClickError struct {
	stage string
	err   error
}

func (e *uploadClickError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("upload: click failed at %s: %v", e.stage, e.err)
	}
	return "upload: click failed at " + e.stage
}

func (e *uploadClickError) Unwrap() error { return e.err }

// formUploadViaButton handles LinkedIn's native-dialog "Upload resume" button layout.
// chooserCtx is derived from the operation ctx so cancellation propagates to the
// file-chooser goroutine. The goroutine is always joined before returning.
func (b *Bot) formUploadViaButton(ctx context.Context, page *rod.Page, filePath string) error {
	absPath, absErr := filepath.Abs(filePath)
	if absErr != nil {
		return fmt.Errorf("upload: resolve absolute path: %w", absErr)
	}
	filePath = absPath

	chooserCtx, chooserCancel := context.WithCancel(ctx)
	defer chooserCancel()
	ctxPage := page.Context(chooserCtx)

	var chooserEvent proto.PageFileChooserOpened
	chooserReady := ctxPage.WaitEvent(&chooserEvent)

	interceptOn := proto.PageSetInterceptFileChooserDialog{Enabled: true}
	if err := interceptOn.Call(page); err != nil {
		return fmt.Errorf("linkedin: file chooser intercept setup: %w", err)
	}
	defer func() { _ = proto.PageSetInterceptFileChooserDialog{Enabled: false}.Call(page) }()

	// Capture pre-upload state. Section may render before radio children appear;
	// retry up to 3 times. Empty or unverifiable libraries are held for manual review.
	base := strings.ToLower(filepath.Base(filePath))
	var preFilenameExisted bool
	preUploadOK := false
	const preUploadAttempts = 8
	for attempt := 0; attempt < preUploadAttempts; attempt++ {
		r, evalErr := ctxPage.Timeout(3*time.Second).Eval(jsLinkedInResumePreUploadState, base)
		if evalErr != nil {
			return fmt.Errorf("upload: cannot read pre-upload resume state: %w", evalErr)
		}
		if r.Value.Get("stateVerified").Bool() {
			preFilenameExisted = r.Value.Get("filenameExisted").Bool()
			preUploadOK = true
			break
		}
		if !r.Value.Get("sectionFound").Bool() {
			if attempt < preUploadAttempts-1 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}
			if btnProbe, probeErr := ctxPage.Timeout(3 * time.Second).ElementByJS(rod.Eval(jsLinkedInFindUploadButton)); probeErr == nil && btnProbe != nil {
				b.log.Info().Str("filename", base).Msg("linkedin: upload button found without resolved section — proceeding")
				preUploadOK = true
				preFilenameExisted = false
				break
			}
			return b.holdUpload(uploadStageSectionNotFound, base)
		}
		// Section found but no radios — possibly still loading. Retry.
		if attempt < preUploadAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	if !preUploadOK {
		return b.holdUpload(uploadStageNoRadios, base)
	}

	// Find the upload button element for a trusted Rod click.
	// Rod's Click uses CDP Input.dispatchMouseEvent — a trusted user gesture.
	// This is required so that <input type=file>.click() inside the handler opens the chooser.
	btn, btnErr := ctxPage.Timeout(5 * time.Second).ElementByJS(rod.Eval(jsLinkedInFindUploadButton))
	if btnErr != nil {
		return fmt.Errorf("upload: button not found: %w", btnErr)
	}
	// Instrumented click: observe each Rod sub-stage separately so the failing one is
	// identifiable from the logs. Preserves Rod's behaviour — ScrollIntoView already includes a
	// stability wait, and Hover performs the retrying interactability check + mouse move exactly
	// as Element.Click does. Each stage is bounded by a fresh child of the live preparation ctx
	// (not the element's inherited 5s), so an internal stage timeout surfaces while ctx is live.
	if clickErr := b.instrumentedUploadClick(ctx, page, btn, base); clickErr != nil {
		return clickErr
	}

	waitCh := make(chan error, 1)
	go func() {
		chooserReady()
		if chooserCtx.Err() != nil {
			waitCh <- chooserCtx.Err()
			return
		}
		waitCh <- proto.DOMSetFileInputFiles{
			Files:         []string{filePath},
			BackendNodeID: chooserEvent.BackendNodeID,
		}.Call(ctxPage)
	}()

	// Brief DOM-settle before checking for a limit indicator.
	time.Sleep(400 * time.Millisecond)
	if limitErr := linkedInHandleResumeLimitDialog(page); limitErr != nil {
		chooserCancel()
		// waitCh is buffered(1); worker sends once and exits. No drain needed.
		return limitErr
	}

	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		chooserCancel()
		return ctx.Err()
	case err := <-waitCh:
		chooserCancel()
		if err != nil {
			return fmt.Errorf("linkedin: file chooser deliver: %w", err)
		}
	case <-timer.C:
		chooserCancel()
		return fmt.Errorf("linkedin: file chooser did not open within 12 s — " +
			"upload mechanism may differ from CDP file-chooser; inspect live DOM")
	}

	if err := linkedInWaitForResumeAccepted(ctx, page, filePath, !preFilenameExisted); err != nil {
		var h *uploadIdentityHold
		if errors.As(err, &h) {
			return b.holdUpload(h.stage, h.filename)
		}
		return err
	}
	// Duplicate-filename guard: if the filename existed BEFORE the upload we cannot
	// confirm a fresh upload occurred.
	// Exception: version-scoped filenames (stem_<8hex>.ext) encode the document version;
	// a pre-existing+selected entry means the same version was prepared before â safe to reuse.
	if preFilenameExisted {
		if isVersionScopedFilename(base) {
			res2, evalErr2 := page.Timeout(3*time.Second).Eval(jsLinkedInCheckResumeAccepted, base)
			if evalErr2 == nil && res2.Value.Get("accepted").Bool() && res2.Value.Get("filenameVisible").Bool() {
				log.Info().Str("filename", base).Msg("linkedin: version already in library and selected; reusing without re-upload")
				return nil
			}
			return b.holdUpload(uploadStageVersionScopedNotSel, base)
		}
		return b.holdUpload(uploadStageNotVersionScoped, base)
	}
	return nil
}

// linkedInHandleResumeLimitDialog checks for a resume-library-full indicator immediately
// after the upload button is clicked. Returns an actionable error if found, nil otherwise.
// The selectors are unconfirmed — update them after observing the limit dialog in live DOM.
func linkedInHandleResumeLimitDialog(page *rod.Page) error {
	res, err := page.Timeout(2 * time.Second).Eval(`() => {
		// [UNCONFIRMED] — verify selector from live LinkedIn DOM when library is full.
		return !!(document.querySelector('[aria-label*="resume limit" i],[data-test*="resume-limit"],[data-testid*="resume-limit"]'));
	}`)
	if err != nil {
		return nil // eval error means no limit element — not a limit situation
	}
	if res.Value.Bool() {
		return fmt.Errorf("linkedin: resume library full — remove an old resume from your LinkedIn profile and retry")
	}
	return nil
}

// linkedInPageEvalMu serializes LinkedIn resume polling evals. Concurrent Eval calls
// during waitForResumeAccepted previously stacked on the renderer and could crash Chrome.
var linkedInPageEvalMu sync.Mutex

func linkedInPageEval(page *rod.Page, timeout time.Duration, js string, args ...interface{}) (*proto.RuntimeRemoteObject, error) {
	linkedInPageEvalMu.Lock()
	defer linkedInPageEvalMu.Unlock()
	return page.Timeout(timeout).Eval(js, args...)
}

// linkedInWaitForResumeAccepted polls within the resolved container for evidence
// that LinkedIn accepted the uploaded resume. Verifies the accepted entry matches
// filePath by checking the checked radio label. Returns errUploadIdentityUnverifiable
// (a HOLD sentinel) when the file was accepted but identity cannot be confirmed.
func linkedInWaitForResumeAccepted(ctx context.Context, page *rod.Page, filePath string, selectionAllowed ...bool) error {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := linkedInWaitForResumeAcceptedPoll(bounded, page.Context(bounded), filePath, selectionAllowed...)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return &uploadIdentityHold{stage: uploadStageAcceptanceTimeout, filename: strings.ToLower(filepath.Base(filePath))}
	}
	return err
}

func linkedInWaitForResumeAcceptedPoll(ctx context.Context, page *rod.Page, filePath string, selectionAllowed ...bool) error {
	allowSelection := len(selectionAllowed) == 0 || selectionAllowed[0]
	base := filePath
	if i := strings.LastIndexAny(filePath, "/\\"); i >= 0 {
		base = filePath[i+1:]
	}
	deadline := time.Now().Add(30 * time.Second)
	var mismatchSince time.Time
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, evalErr := linkedInPageEval(page, 2*time.Second, jsLinkedInCheckResumeAcceptedLite, base)
		if evalErr != nil {
			log.Warn().Err(evalErr).Msg("linkedin: waitForResumeAccepted eval error")
		} else if res != nil {
			if res.Value.Get("noDialog").Bool() {
				saveApplyDebug(page, "resume_dialog_closed")
				return fmt.Errorf("linkedin: easy apply dialog closed during resume upload (LinkedIn may have rejected the file)")
			}
			if res.Value.Get("error").Bool() {
				msg := res.Value.Get("message").String()
				if msg == "" {
					msg = "upload rejected by LinkedIn (error indicator visible)"
				}
				saveApplyDebug(page, "resume_upload_rejected")
				return fmt.Errorf("linkedin: %s", msg)
			}
			if res.Value.Get("uploading").Bool() {
				mismatchSince = time.Time{}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}
			if res.Value.Get("accepted").Bool() && res.Value.Get("filenameVisible").Bool() {
				log.Info().Str("filename", base).Msg("linkedin: resume upload accepted and identity verified")
				return nil
			}
			// A newly observed exact entry may be selected after upload completes.
			// Selecting a pre-existing entry would manufacture acceptance evidence.
			if allowSelection && res.Value.Get("inList").Bool() {
				if _, selErr := linkedInPageEval(page, 2*time.Second, jsLinkedInSelectResumeRadioByFilename, base); selErr == nil {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(300 * time.Millisecond):
					}
					res2, err2 := linkedInPageEval(page, 2*time.Second, jsLinkedInCheckResumeAcceptedLite, base)
					if err2 == nil && res2 != nil && res2.Value.Get("accepted").Bool() && res2.Value.Get("filenameVisible").Bool() {
						log.Info().Str("filename", base).Msg("linkedin: resume selected after upload appeared in library")
						return nil
					}
				}
			}
			if res.Value.Get("identityMismatch").Bool() {
				// Upload completion and the selected library entry may hydrate in
				// separate renders. Require a stable completed mismatch before hold.
				if mismatchSince.IsZero() {
					mismatchSince = time.Now()
				} else if time.Since(mismatchSince) >= time.Second {
					return &uploadIdentityHold{stage: uploadStagePostUploadMismatch, filename: base}
				}
			} else {
				mismatchSince = time.Time{}
			}
		}
		if msgs := pageValidationErrors(page); len(msgs) > 0 {
			for _, m := range msgs {
				lower := strings.ToLower(m)
				if strings.Contains(lower, "upload") || strings.Contains(lower, "resume") {
					saveApplyDebug(page, "resume_upload_rejected")
					return fmt.Errorf("linkedin: %s", m)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	log.Warn().Str("filename", base).Msg("linkedin: resume acceptance not confirmed after polling")
	saveApplyDebug(page, "resume_not_accepted")
	return &uploadIdentityHold{stage: uploadStageAcceptanceTimeout, filename: base}
}

func (b *Bot) fillFormStepFromVision(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (filled bool, err error) {
	if b.cfg.ApplicationAnswers != nil {
		return b.fillRecoveredLegacyControls(ctx, page, lazy)
	}
	if b.cfg.Tailor == nil || page == nil {
		return false, nil
	}
	shot, err := page.Screenshot(false, nil)
	if err != nil || len(shot) == 0 {
		return false, err
	}
	vctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	identified, err := b.cfg.Tailor.IdentifyFormFields(vctx, shot)
	if err != nil {
		return false, err
	}
	if len(identified) == 0 {
		return false, nil
	}
	log.Info().Int("count", len(identified)).Msg("form: vision identified fields")
	for _, idf := range identified {
		var options []string
		if len(idf.Options) > 0 {
			options = idf.Options
		}
		answer, qErr := b.answerFormQuestion(ctx, lazy, idf.Question, options)
		if qErr != nil {
			return filled, qErr
		}
		if strings.TrimSpace(answer) == "" {
			continue
		}
		res, evalErr := page.Timeout(5*time.Second).Eval(jsFillByQuestionAnswer, idf.Question, answer, idf.Type)
		if evalErr != nil || res.Value.Nil() || !res.Value.Bool() {
			log.Warn().Str("question", idf.Question).Str("type", idf.Type).Msg("form: vision fill could not locate field")
			continue
		}
		log.Info().Str("question", idf.Question).Str("type", idf.Type).Msg("form: vision filled field")
		filled = true
	}
	return filled, nil
}

// ── answerFormQuestion ────────────────────────────────────────────────────────

// answerFormQuestion returns the best answer for a form field.
// options is non-nil for radio/select, the returned string should match one
// of the option labels.  For free-text fields options is nil.
//
// Contact facts and simple sponsorship questions can resolve directly. Legal
// work rights and experience durations need independently grounded answers;
// sponsorship and career totals cannot establish those facts.
func (b *Bot) answerFormQuestion(ctx context.Context, lazy *lazyDocGen, question string, options []string) (string, error) {
	if answer, ok := preparedLegacyAnswer(lazy, question, options); ok {
		return answer, nil
	}
	if b.cfg.ApplicationAnswers != nil {
		return "", errUnresolvedLegacyControl
	}
	if lazy != nil && lazy.answerPlan != nil && lazy.answerActiveKey != "" {
		return "", errAdaptiveQuestionReview
	}
	if manualDeclaration(SnapField{Label: question}) {
		return "", errAdaptiveQuestionReview
	}
	// Contact-info fields (phone, email, name, city) — use profile, never LLM prose.
	if len(options) == 0 {
		if profile := b.currentProfile(); profile != nil {
			if ans := profileContactAnswer(question, inferContactInputType(question), &profile.PersonalInformation); ans != "" {
				log.Info().Str("question", question).Msg("form: answered via heuristic (contact info)")
				return sanitizeContactFieldAnswer(ans, question, inferContactInputType(question)), nil
			}
		}
	}

	sensitive := legacySensitiveQuestion(question)
	if sensitive {
		field := SnapField{Ref: "f0", Label: question, Type: SnapTypeText, Required: true, Options: options}
		if len(options) > 0 {
			field.Type = SnapTypeSelect
		} else if matchesAny(strings.ToLower(question), "how many years", "years of", "years experience") {
			field.Type = SnapTypeNumber
		}
		if lazy != nil {
			field.JobLocation, field.JobTitle = lazy.job.Location, lazy.job.Title
		}
		profile := b.currentProfile()
		if local, ok := deterministicResolve(field, profile); ok && local.Action == ActionFill && !local.Pending {
			return validatedLegacyAnswer(local.Value, options)
		}
		if b.cfg.AdaptiveLLM != nil {
			return b.groundedLegacyAnswer(ctx, lazy, field, profile)
		}
	}

	// Ordinary wording still uses the Legacy provider. Sensitive positive
	// answers from a provider without a grounding verifier cannot be trusted.
	if b.cfg.Tailor == nil || lazy == nil {
		log.Debug().Bool("tailor_nil", b.cfg.Tailor == nil).Bool("lazy_nil", lazy == nil).
			Str("question", question).Msg("form: LLM skipped (not configured)")
	} else {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		answer, err := b.cfg.Tailor.AnswerFormQuestion(b.llmCtx(llmCtx, "form question", lazy.job.ID), lazy.formProfileJSON(), question, options)
		if err != nil {
			return "", fmt.Errorf("form question: %w", err)
		}
		if !sensitive {
			return validatedLegacyAnswer(answer, options)
		}
	}
	return "", errAdaptiveQuestionReview
}

func legacySensitiveQuestion(question string) bool {
	return isSalaryFormQuestion(question) || matchesAny(strings.ToLower(question), "sponsor", "visa", "authoris", "authoriz", "legally permitted", "eligible to work", "right to work", "legally able", "work without", "working rights", "work permit", "how many years", "years of", "years experience")
}

func validatedLegacyAnswer(answer string, options []string) (string, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", errAdaptiveQuestionReview
	}
	if len(options) == 0 {
		return answer, nil
	}
	matched, matches := "", 0
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option), answer) {
			matched, matches = option, matches+1
		}
	}
	if matches != 1 {
		return "", fmt.Errorf("%w: answer does not identify one supplied option", errAdaptiveQuestionReview)
	}
	return matched, nil
}

func (b *Bot) groundedLegacyAnswer(ctx context.Context, lazy *lazyDocGen, field SnapField, profile *domain.ResumeProfile) (string, error) {
	snap := StepSnapshot{V: SnapshotSchemaV1, Platform: string(b.cfg.Platform), Fields: []SnapField{field}}
	jobID := ""
	if lazy != nil {
		jobID, snap.EmployerKey = lazy.job.ID, "legacy-question:"+lazy.job.Company
	}
	llmCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	proposals, err := boundFormLLM(b.cfg.AdaptiveLLM).InterpretFields(b.llmCtx(llmCtx, "form question", jobID), profile, snap, []string{field.Ref})
	if err != nil {
		return "", fmt.Errorf("form question: %w", err)
	}
	for _, resolution := range validateLLMProposals(proposals, []string{field.Ref}, snap, profile) {
		if resolution.Action == ActionFill && !resolution.Pending && validGroundedProof(resolution.grounded, profile, snap, field, resolution.IntentType, resolution.BindingKey, resolution.Value) {
			return validatedLegacyAnswer(resolution.Value, field.Options)
		}
	}
	return "", errAdaptiveQuestionReview
}

// totalExperienceYears sums experience from the profile's ExperienceDetails.
// EmploymentPeriod is expected in formats like "Jan 2019 - Mar 2023" or "2019 - Present".
func totalExperienceYears(profile *domain.ResumeProfile) int {
	if profile == nil {
		return 0
	}
	currentYear := time.Now().Year()
	total := 0
	for _, exp := range profile.ExperienceDetails {
		start, end := parseEmploymentYears(exp.EmploymentPeriod, currentYear)
		if start > 0 && end >= start {
			total += end - start
		}
	}
	return total
}

// parseEmploymentYears extracts start/end years from strings like:
// "Jan 2019 - Mar 2023", "2019 - Present", "2020 - 2022"
func parseEmploymentYears(period string, currentYear int) (start, end int) {
	if period == "" {
		return 0, 0
	}
	// Find all 4-digit years in the string.
	var years []int
	for i := 0; i+4 <= len(period); i++ {
		y := 0
		for j := 0; j < 4; j++ {
			c := period[i+j]
			if c < '0' || c > '9' {
				y = 0
				break
			}
			y = y*10 + int(c-'0')
		}
		if y >= 1970 && y <= currentYear+1 {
			years = append(years, y)
			i += 3 // skip past this year
		}
	}
	lower := strings.ToLower(period)
	isPresent := strings.Contains(lower, "present") || strings.Contains(lower, "current") || strings.Contains(lower, "now")
	switch len(years) {
	case 0:
		if isPresent {
			return currentYear, currentYear
		}
		return 0, 0
	case 1:
		if isPresent {
			return years[0], currentYear
		}
		return years[0], years[0]
	default:
		endY := years[len(years)-1]
		if isPresent {
			endY = currentYear
		}
		return years[0], endY
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

var rePhoneChunk = regexp.MustCompile(`\+?\d[\d\s().-]{6,}\d`)

// profileContactAnswer returns a profile value for standard contact-info form fields.
func profileContactAnswer(question, inputType string, pi *domain.PersonalInformation) string {
	if pi == nil {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(question))
	if lower == "" && inputType == "" {
		return ""
	}
	switch countryWords(lower) {
	case "city", "town", "city town", "location city":
		return strings.TrimSpace(pi.City)
	case "country", "country of residence":
		return strings.TrimSpace(pi.Country)
	}

	if isPhoneField(question, inputType) {
		phone := strings.TrimSpace(pi.Phone)
		if phone == "" {
			return ""
		}
		prefix := strings.TrimSpace(pi.PhonePrefix)
		if prefix != "" && !strings.HasPrefix(phone, "+") {
			return sanitizePhoneAnswer(strings.TrimSpace(prefix + " " + phone))
		}
		return sanitizePhoneAnswer(phone)
	}

	if inputType == "email" || containsAny(lower, "email", "e-mail") {
		return strings.TrimSpace(pi.Email)
	}

	if containsAny(lower, "first name", "given name", "forename") {
		return strings.TrimSpace(pi.Name)
	}
	if containsAny(lower, "last name", "family name", "surname") {
		return strings.TrimSpace(pi.Surname)
	}
	if matches := countryWords(lower); matches == "city" || matches == "town" || matches == "city town" || matches == "location city" {
		return strings.TrimSpace(pi.City)
	}

	if isLocationField(lower) {
		return profileLocation(pi)
	}

	return ""
}

func isPhoneField(question, inputType string) bool {
	if inputType == "tel" {
		return true
	}
	lower := strings.ToLower(question)
	return containsAny(lower, "phone", "mobile", "cell", "telephone") &&
		!containsAny(lower, "years", "experience", "smartphone")
}

func isLocationField(lower string) bool {
	return (strings.Contains(lower, "location") || strings.Contains(lower, "city")) &&
		!containsAny(lower, "relocate", "willing to", "preferred work", "work location preference")
}

func profileLocation(pi *domain.PersonalInformation) string {
	city := strings.TrimSpace(pi.City)
	country := strings.TrimSpace(pi.Country)
	if city != "" && country != "" {
		return city + ", " + country
	}
	if city != "" {
		return city
	}
	return strings.TrimSpace(pi.Address)
}

func inferContactInputType(question string) string {
	if isPhoneField(question, "") {
		return "tel"
	}
	lower := strings.ToLower(question)
	if containsAny(lower, "email", "e-mail") {
		return "email"
	}
	return ""
}

// sanitizePhoneAnswer strips prose and returns digits (optional leading +).
func sanitizePhoneAnswer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := rePhoneChunk.FindString(s); m != "" {
		s = m
	}
	plus := strings.HasPrefix(strings.TrimSpace(s), "+")
	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	out := digits.String()
	if out == "" {
		return ""
	}
	if plus {
		return "+" + out
	}
	return out
}

func sanitizeContactFieldAnswer(answer, question, inputType string) string {
	if isPhoneField(question, inputType) {
		return sanitizePhoneAnswer(answer)
	}
	if inputType == "email" || containsAny(strings.ToLower(question), "email", "e-mail") {
		return strings.TrimSpace(answer)
	}
	return answer
}

// sanitizeNumericFieldAnswer turns an LLM answer into a value acceptable for LinkedIn
// numeric screening fields (0–99). Strips product/version numbers from the question
// context (e.g. "Dynamics 365" must not become years "365").
func sanitizeNumericFieldAnswer(answer, question string) string {
	n := extractFirstNumberMax(answer, maxExperienceYears)
	if n != "0" {
		return n
	}
	// LLM may echo a version from the question — if answer only contains question numbers, use 0.
	qNums := reNumber.FindAllString(question, -1)
	for _, qn := range qNums {
		if !plausibleExperienceYear(qn, maxExperienceYears) {
			trimmed := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(answer, qn, ""), "  ", " "))
			if trimmed == "" || trimmed == "-" {
				return "0"
			}
		}
	}
	return n
}

func looksLikeOpenEndedQuestion(q string) bool {
	lower := strings.ToLower(q)
	needles := []string{
		"describe", "explain", "tell us", "tell me", "please provide",
		"what was the", "how did you", "write a", "share an example",
		"give an example", "in your own words", "walk us through",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	// Long multi-sentence prompts are almost never dropdowns.
	return len(strings.TrimSpace(q)) > 140
}

func matchSelectOption(f formField, answer string) (value string, ok bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" || len(f.Options) == 0 {
		return "", false
	}
	for _, o := range f.Options {
		if strings.EqualFold(strings.TrimSpace(o.Label), answer) || strings.EqualFold(strings.TrimSpace(o.Value), answer) {
			return o.Value, true
		}
	}
	return "", false
}

// fillOpenEndedNearSelect finds a textarea in the same form-element (or matching
// question label) and fills it. Used when LinkedIn essay prompts are mis-scanned
// as selects or when the LLM returns prose that cannot map to dropdown options.
func (b *Bot) fillOpenEndedNearSelect(page *rod.Page, f formField, answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" || page == nil {
		return false
	}
	const js = `(selectId, question, value) => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try { const d = el.contentDocument; if (d) r.push(...allInDOM(d, selector)); } catch(e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		function setNative(el, v) {
			const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
			const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
			el.focus();
			setter.call(el, v);
			el.dispatchEvent(new Event('input', { bubbles: true }));
			el.dispatchEvent(new Event('change', { bubbles: true }));
			el.blur();
		}
		const q = (question || '').toLowerCase().replace(/\s+/g, ' ').trim().slice(0, 80);
		let scope = null;
		if (selectId) {
			const sel = allInDOM(document, 'select').find(s => s.id === selectId);
			if (sel) scope = sel.closest('[data-test-form-element], .fb-form-element, .artdeco-form-element, fieldset') || sel.parentElement;
		}
		let ta = scope ? allInDOM(scope, 'textarea')[0] : null;
		if (!ta && q) {
			const formEls = allInDOM(document, '[data-test-form-element], .fb-form-element, .artdeco-form-element, fieldset');
			for (const fe of formEls) {
				const label = (fe.querySelector('label, legend') || fe).textContent.toLowerCase().replace(/\s+/g, ' ');
				if (label.includes(q.slice(0, 40)) || q.includes(label.slice(0, 40))) {
					ta = allInDOM(fe, 'textarea')[0];
					if (ta) break;
				}
			}
		}
		if (!ta) {
			// Last resort: first empty / invalid textarea in the apply modal.
			ta = allInDOM(document, 'textarea').find(t => !t.value.trim() || t.getAttribute('aria-invalid') === 'true');
		}
		if (!ta) return false;
		setNative(ta, value);
		return true;
	}`
	res, err := page.Eval(js, f.ID, f.Question, answer)
	if err != nil || res == nil || !res.Value.Bool() {
		return false
	}
	log.Info().Str("question", f.Question).Int("chars", len(answer)).Msg("form: open-ended textarea filled")
	return true
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// pickFormOption returns the first option whose lowercase label contains needle,
// or returns needle itself when options is nil (free-text field).
// Ambiguous or missing matches remain unanswered.
func pickFormOption(options []string, needle string) string {
	if options == nil {
		return needle
	}
	lowerNeedle := strings.ToLower(needle)
	answer, matches := "", 0
	for _, o := range options {
		if strings.Contains(strings.ToLower(o), lowerNeedle) {
			answer, matches = o, matches+1
		}
	}
	if matches == 1 {
		return answer
	}
	return ""
}

// isVersionScopedFilename reports whether name matches the version-scoped upload format:
// stem_<8 lowercase hex chars>.ext  (e.g. "resume_abc12345.pdf").
func isVersionScopedFilename(name string) bool {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ext)
	idx := strings.LastIndex(stem, "_")
	if idx < 0 {
		return false
	}
	suffix := stem[idx+1:]
	if len(suffix) != 8 {
		return false
	}
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// looksLikeLinkedInResumeLibraryRadio detects SDUI resume picker groups (options are filenames).
func looksLikeLinkedInResumeLibraryRadio(f formField) bool {
	if len(f.Options) < 1 {
		return false
	}
	for _, o := range f.Options {
		if !linkedInFilenameLike(strings.TrimSpace(o.Label)) {
			return false
		}
	}
	return true
}

func linkedInFilenameLike(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" || !strings.Contains(l, ".") {
		return false
	}
	switch filepath.Ext(l) {
	case ".pdf", ".doc", ".docx", ".rtf", ".txt":
		return true
	default:
		return false
	}
}

// linkedInResumeLibraryRadioField is true for resume-library picker groups on SDUI apply steps.
func linkedInResumeLibraryRadioField(f formField) bool {
	if looksLikeLinkedInResumeLibraryRadio(f) {
		return true
	}
	if linkedInFilenameLike(f.Question) {
		return true
	}
	for _, o := range f.Options {
		if linkedInFilenameLike(o.Label) {
			return true
		}
	}
	return false
}

func (b *Bot) fillLinkedInResumeLibraryRadio(page *rod.Page, f formField, wantFilename string, force bool) bool {
	wantFilename = strings.ToLower(strings.TrimSpace(wantFilename))
	for _, opt := range f.Options {
		label := strings.ToLower(strings.TrimSpace(opt.Label))
		if !resumeLibraryLabelMatches(label, wantFilename) {
			continue
		}
		if opt.ID != "" {
			if r2, err := page.Timeout(12*time.Second).Eval(jsFillRadioByID, opt.ID, force); err == nil && r2.Value.Bool() {
				return true
			}
		}
		if f.Name != "" {
			if r2, err := page.Timeout(12*time.Second).Eval(jsFillRadio, f.Name, opt.Label, force); err == nil && r2.Value.Bool() {
				return true
			}
		}
	}
	return false
}

func resumeLibraryLabelMatches(label, wantFilename string) bool {
	if label == wantFilename {
		return true
	}
	if strings.Contains(label, wantFilename) || strings.Contains(wantFilename, label) {
		return true
	}
	base := strings.TrimSuffix(wantFilename, filepath.Ext(wantFilename))
	return base != "" && strings.Contains(label, base)
}
