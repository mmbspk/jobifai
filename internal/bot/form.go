package bot

// form.go, generic form-field detection and filling used by both LinkedIn Easy
// Apply and Seek Quick Apply.  Works by injecting JS into the live rod page to
// scan the current step, then filling each unanswered field via profile defaults,
// heuristics, or an LLM call.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// ── Data types ────────────────────────────────────────────────────────────────

type formFieldOption struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Label string `json:"label"`
}

type formField struct {
	Type      string            `json:"type"`        // file | radio | select | text
	Index     int               `json:"index"`       // ordinal among same type
	Name      string            `json:"name"`        // radio group name
	ID        string            `json:"id"`          // select / text element id
	Question  string            `json:"question"`    // visible label text
	Options   []formFieldOption `json:"options"`     // radio / select choices
	HasFile   bool              `json:"hasFile"`     // file already attached
	InputType string            `json:"inputType"`   // text | number | textarea
	Typeahead bool              `json:"isTypeahead"` // combobox requiring dropdown selection
}

func (f formField) optionLabels() []string {
	labels := make([]string, len(f.Options))
	for i, o := range f.Options {
		labels[i] = o.Label
	}
	return labels
}

// ── JavaScript constants ───────────────────────────────────────────────────────

// jsScanFields returns an array of unanswered form fields on the current step.
// Compatible with LinkedIn's Artdeco modal (shadow DOM) and Seek's plain-HTML form.
const jsScanFields = `() => {
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
				if (s.display === 'none' || s.visibility === 'hidden') return false;
				node = node.parentElement;
			}
			return true;
		} catch(e) { return false; }
	}

	// Anchor on platform-specific apply-form selectors, VISIBLE only.
	// Seek anchors are listed first; they don't match LinkedIn's DOM, so on
	// LinkedIn the next anchors win. Conversely, on Seek the LinkedIn anchors
	// don't match, so we land on a Seek-specific container and don't pick up
	// the search/refine sidebar's <form>.
	const anchor =
		allInDOM(document, "[data-testid='quick-apply-form']").find(isVisible) ||
		allInDOM(document, "[data-automation='apply-form']").find(isVisible) ||
		allInDOM(document, "form[data-testid*='applicationForm']").find(isVisible) ||
		allInDOM(document, '[data-test-form-element]').find(isVisible) ||
		allInDOM(document, 'fieldset[data-test-form-builder-radio-button-form-component]').find(isVisible) ||
		allInDOM(document, '.jobs-easy-apply-content').find(isVisible) ||
		allInDOM(document, '.jobs-easy-apply-modal').find(isVisible);

	let container = document.body;
	if (anchor) {
		container = anchor.closest('form') || anchor.closest('.jobs-easy-apply-content') || anchor.parentElement || anchor;
	} else {
		// Semantic fallback: when all platform-specific selectors miss, scope to
		// any visible dialog or form with at least one required input. Skip
		// candidates that look like search/refine UI (Seek's job-search page has
		// a sidebar <form> with "Show salary range refinements" / "Pay" radios).
		function looksLikeSearchUI(scope) {
			if (!scope) return false;
			if (scope.closest("[role='search'], [data-automation*='searchBar'], [data-automation*='refine'], [data-automation*='filter']")) return true;
			const head = scope.querySelector('legend, h1, h2, h3, [role="heading"]');
			const txt = (head && head.textContent || '').toLowerCase();
			return txt.includes('refinement') || txt.includes('sort by') || txt.includes('keyword') || txt.includes('classification');
		}
		const dialog = document.querySelector('[role="dialog"]');
		const form = dialog ? dialog.querySelector('form') : document.querySelector('form');
		const scope = form || dialog;
		if (scope && !looksLikeSearchUI(scope)) {
			// Match any non-hidden input — Seek doesn't set [required] even for
			// mandatory fields, so we can't key off required alone.
			const hasInputs = [...scope.querySelectorAll(
				'input:not([type="hidden"]), select, textarea'
			)].some(isVisible);
			if (hasInputs) {
				container = scope;
				console.warn('jsScanFields: using semantic fallback container');
			}
		}
	}

	const isSeekApply = !!(container.closest && container.closest(
		"[data-testid='quick-apply-form'], [data-automation='apply-form'], form[data-testid*='applicationForm']"
	));

	// Use htmlFor property to handle IDs with special characters like ":" (LinkedIn React IDs)
	function labelFor(id) {
		if (!id) return null;
		return [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === id) || null;
	}

	// ── Review-only steps (Work Experience / Education cards), no inputs, skip immediately ──
	// These steps show read-only cards with Edit/Remove buttons. There are no form fields
	// to fill; the bot just needs to click Next/Review.
	if (allInDOM(document, '.jobs-easy-apply-repeatable-groupings__groupings').some(isVisible)) {
		return JSON.stringify([]);
	}
	// LinkedIn "Review your application" final page: contact/resume are read-only
	// summaries — not unanswered fields. Return empty so Submit is not blocked.
	{
		const pageText = (document.body && document.body.innerText || '').toLowerCase();
		if (pageText.includes('review your application')) {
			return JSON.stringify([]);
		}
	}

	const fields = [];

	// ── File inputs, scan globally via shadow DOM (LinkedIn hides these in shadow roots) ───
	// Filter to document uploads only (PDF/DOC/TXT), skip image/video/etc.
	// Use isContainerVisible so we don't pick up file inputs from off-screen steps.
	allInDOM(document, 'input[type="file"]').filter(el => {
		const a = (el.accept || '').toLowerCase();
		const typeOk = !a || a.includes('pdf') || a.includes('doc') || a.includes('txt') || a.includes('rtf');
		return typeOk && isContainerVisible(el);
	}).forEach((el, i) => {
		fields.push({ type: 'file', index: i, hasFile: !!(el.files && el.files.length > 0) });
	});

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

	const seenRadio = new Set();
	allInDOM(container, 'input[type="radio"]').filter(isContainerVisible).forEach(r => {
		const groupKey = r.name || r.id;
		if (!groupKey || seenRadio.has(groupKey)) return;
		seenRadio.add(groupKey);

		let all;
		if (r.name) {
			// Use JS property comparison, CSS selectors break on URN-style names
			// containing colons and parentheses (LinkedIn's form element names).
			all = allInDOM(container, 'input[type="radio"]').filter(x => x.name === r.name);
		} else {
			const parent = r.closest('[data-test-form-element], .artdeco-form-element, fieldset, [role="group"]') || r.parentElement?.parentElement;
			all = parent ? allInDOM(parent, 'input[type="radio"]') : [r];
		}
		// Filter to same-group members whose container is visible.
		all = all.filter(isContainerVisible);
		if (all.some(x => x.checked || x.getAttribute('aria-checked') === 'true')) return;

		const grp = r.closest('fieldset, [role="group"], .fb-form-element, [data-test-form-element], .artdeco-form-element')
		            || r.parentElement?.parentElement;

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

		const options = all.map(x => {
			const lbl = labelFor(x.id) || x.closest('label') || x.parentElement;
			return { id: x.id, value: x.value, label: (lbl ? lbl.textContent : x.value).trim() };
		});
		fields.push({ type: 'radio', name: r.name || groupKey, question, options });
	});

	// ── Checkbox groups (Lever ATS / LinkedIn consent questions) ─────────────
	// Group by shared fieldset. Skip LinkedIn's "follow company" checkbox.
	const seenCb = new Set();
	allInDOM(container, 'input[type="checkbox"]').filter(isContainerVisible).forEach(cb => {
		if (cb.id === 'follow-company-checkbox' || (cb.name || '').includes('follow-company')) return;
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
		if (all.length === 0 || all.every(x => x.checked)) return;

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
		fields.push({ type: 'checkbox', name: cb.name || groupKey, question, options });
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
		if (!blank && sel.getAttribute('aria-invalid') !== 'true') return;
		const lbl = labelFor(sel.id)
		            || (formEl && formEl.querySelector('label, legend, .fb-form-element-label, span[aria-hidden="true"]'));
		const opts = [...sel.options]
			.filter(o => !o.disabled && o.value !== '')
			.map(o => ({ value: o.value, label: o.text.trim() }));
		if (opts.length === 0) return;
		fields.push({ type: 'select', id: sel.id,
		              question: lbl ? lbl.textContent.trim().replace(/\s+/g, ' ') : '', options: opts });
	});

	// ── Text / number / tel / email / textarea that are empty ─────────────────
	// Track a per-page index so duplicate element IDs (LinkedIn reuses the same ID
	// for every text input) can be disambiguated when filling.
	let textIdx = 0;
	allInDOM(container, 'input[type="text"], input[type="number"], input[type="tel"], input[type="email"], textarea').filter(el => {
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
		if (el.value.trim() && !isTypeahead && el.getAttribute('aria-invalid') !== 'true') { textIdx++; return; }
		const isRequired = el.required || el.getAttribute('aria-required') === 'true'
		                || el.getAttribute('aria-invalid') === 'true';
		const formEl = el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element, fieldset');
		const lbl = labelFor(el.id)
		            || (formEl && formEl.querySelector('label, legend, .fb-form-element-label, span[data-test-form-element-label]'))
		            || el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element')?.querySelector('label');
		// Seek Quick Apply often omits required/labels on screening inputs — include
		// any empty field inside the apply form container.
		if (!isRequired && !lbl && el.tagName !== 'TEXTAREA' && !isSeekApply) { textIdx++; return; }
		const question = lbl
			? lbl.textContent.trim().replace(/\s+/g, ' ')
			: (el.placeholder || el.getAttribute('aria-label') || el.getAttribute('name') || '');
		if (!question && el.tagName !== 'TEXTAREA') { textIdx++; return; }
		fields.push({ type: 'text', id: el.id, index: textIdx,
		              question: question || el.placeholder || 'Additional details',
		              isTypeahead,
		              inputType: (el.id.endsWith('-numeric') || el.type === 'number' || el.getAttribute('inputmode') === 'numeric')
		                         ? 'numeric'
		                         : (el.tagName === 'TEXTAREA' ? 'textarea' : (el.type || 'text')) });
		textIdx++;
	});

	return JSON.stringify(fields);
}`

// jsFillRadio clicks a radio button by group name and matching option value/label.
// Uses shadow DOM walking to find elements inside LinkedIn's artdeco modal.
const jsFillRadio = `(name, value) => {
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
	if (isSelected(target)) return true; // already selected, don't toggle
	// Prefer label click — custom radio UIs respond more reliably than input.click().
	const lbl = [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === target.id);
	if (lbl) { lbl.click(); } else { target.click(); }
	target.dispatchEvent(new Event('change', { bubbles: true }));
	target.dispatchEvent(new Event('input',  { bubbles: true }));
	return true;
}`

// jsFillRadioByID clicks a specific radio input by its element ID.
// More reliable than jsFillRadio when names contain special characters or are missing.
const jsFillRadioByID = `(id) => {
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
	const el = allInDOM(document, 'input[type="radio"]').find(r => r.id === id);
	if (!el) return false;
	if (el.checked || el.getAttribute('aria-checked') === 'true') return true;
	// Prefer clicking the associated label — custom radio UIs (Seek) build their
	// visual from label-adjacent elements and respond more reliably to label clicks.
	const lbl = [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === id);
	if (lbl) { lbl.click(); } else { el.click(); }
	el.dispatchEvent(new Event('change', { bubbles: true }));
	el.dispatchEvent(new Event('input',  { bubbles: true }));
	return true;
}`

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

// jsFillFileNth marks the nth document-upload file input for rod to locate.
// Uses the same accept filter as jsScanFields so indices stay consistent.
const jsFillFileNth = `(index) => {
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
	const inputs = allInDOM(document, 'input[type="file"]').filter(el => {
		const a = (el.accept || '').toLowerCase();
		return !a || a.includes('pdf') || a.includes('doc') || a.includes('txt') || a.includes('rtf');
	});
	if (index >= inputs.length) return null;
	inputs[index].setAttribute('data-rod-upload-target', String(index));
	return true;
}`

// ── fillFormStep ──────────────────────────────────────────────────────────────

// extractFirstNumber returns the first integer or decimal number found in s.
// Used to clean up verbose LLM answers for numeric form fields (e.g. "14 years" → "14").
var reNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)
var reYearsNum = regexp.MustCompile(`(?i)(?:^|\s)(\d+(?:\.\d+)?)\s*\+?\s*years?`)
var reYearsRange = regexp.MustCompile(`(?i)(?:^|\s)(\d+(?:\.\d+)?)\s*(?:to|–|-)\s*(\d+(?:\.\d+)?)\s*years?`)

func extractFirstNumber(s string) string {
	if m := reYearsRange.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	if m := reYearsNum.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	nums := reNumber.FindAllString(s, -1)
	// Prefer plausible experience-year values over product/version numbers (e.g. "365" in Dynamics 365).
	for _, n := range nums {
		base := strings.Split(n, ".")[0]
		if v, err := strconv.Atoi(base); err == nil && v <= 50 {
			return n
		}
	}
	if len(nums) > 0 {
		return nums[0]
	}
	return "0" // no digit found → "no experience" = 0; prevents prose text in numeric fields
}

// fillFormStep scans the current form page/modal step for unanswered fields and
// fills them using profile defaults, heuristics, or LLM.
//
// Returns (filled, hasFields):
//   - filled=true  when at least one field was successfully written
//   - hasFields=true when the scan found at least one field (even if none could be filled)
func (b *Bot) fillFormStep(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (filled bool, hasFields bool) {
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

		res, err := p.Timeout(10 * time.Second).Eval(jsScanFields)
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
		const jsHasVisibleFormContent = `() => {
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
			const pageText = (document.body && document.body.innerText || '').toLowerCase();
			// Review page is intentionally field-empty for our scanner.
			if (pageText.includes('review your application')) return 0;
			const root = document.querySelector('[role="dialog"], form') || document.body;
			return [...root.querySelectorAll('input:not([type="hidden"]), select, textarea')]
				.filter(el => isVisible(el) && (!el.value.trim() || el.getAttribute('aria-invalid') === 'true')).length;
		}`
		hasVisibleButUnscanned := false
		for _, p := range pages {
			if probe, err := p.Timeout(10 * time.Second).Eval(jsHasVisibleFormContent); err == nil {
				if n := probe.Value.Int(); n > 0 {
					log.Warn().Int("visible_inputs", n).Msg("form: jsScanFields returned 0 fields but DOM has visible inputs, possible selector break")
					if shot, err := page.Screenshot(false, nil); err == nil {
						_ = os.WriteFile("debug_zero_fields.png", shot, 0o644)
						log.Warn().Msg("form: screenshot saved for selector-break diagnosis")
					}
					hasVisibleButUnscanned = true
					break
				}
			}
		}
		return false, hasVisibleButUnscanned
	}
	// Log each found field so we can debug what's being detected.
	for _, f := range fields {
		log.Debug().Msgf("form: field found type=%s q=%q inputType=%s", f.Type, f.Question, f.InputType)
	}
	log.Info().Int("count", len(fields)).Msg("form: fields to fill")
	hasFields = true

	fileUploadIdx := 0 // track how many file inputs we've processed

	for _, f := range fields {
		b.shortPause() // human-like pause before each field interaction
		switch f.Type {

		case "file":
			resumePath, coverPath := lazy.get() // generates on first call, cached after
			path := resumePath
			if fileUploadIdx > 0 {
				path = coverPath // second file input → cover letter
			}
			if path == "" {
				if f.HasFile {
					// File already attached (LinkedIn pre-populated with existing resume).
					// Accept it as-is so the step doesn't count as unfilled.
					log.Debug().Int("index", f.Index).Msg("form: file input already has attachment, skipping upload")
					filled = true
				} else {
					log.Warn().Int("index", f.Index).Msg("form: no generated PDF path, skipping file upload (existing LinkedIn resume will be used)")
				}
				fileUploadIdx++
				continue
			}
			// Always upload our generated file, replace LinkedIn's default resume.
			if err := b.formUploadFile(page, path, f.Index); err != nil {
				log.Warn().Err(err).Int("index", f.Index).Msg("form: file upload failed")
			} else {
				kind := "resume"
				if fileUploadIdx > 0 {
					kind = "cover letter"
				}
				log.Info().Str("path", path).Str("kind", kind).Msg("form: file uploaded")
				filled = true
			}
			fileUploadIdx++

		case "radio":
			answer := b.answerFormQuestion(ctx, lazy, f.Question, f.optionLabels())
			if answer == "" && len(f.Options) > 0 {
				answer = f.Options[0].Label
				log.Warn().Str("question", f.Question).Str("fallback", f.Options[0].Label).Msg("form: radio fallback to first option")
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
			if optionID == "" && len(f.Options) > 0 {
				optionID = f.Options[0].ID
			}
			radioOK := false
			if optionID != "" {
				if r2, err := page.Eval(jsFillRadioByID, optionID); err == nil && r2.Value.Bool() {
					radioOK = true
				}
			}
			if !radioOK {
				// Fallback: fill by group name.
				if r2, err := page.Eval(jsFillRadio, f.Name, answer); err == nil && r2.Value.Bool() {
					radioOK = true
				}
			}
			if !radioOK {
				log.Warn().Str("question", f.Question).Str("answer", answer).Str("optionID", optionID).Msg("form: radio fill failed")
				continue
			}
			log.Info().Str("question", f.Question).Str("answer", answer).Msg("form: radio answered")
			filled = true

		case "checkbox":
			answer := b.answerFormQuestion(ctx, lazy, f.Question, f.optionLabels())
			if answer == "" && len(f.Options) > 0 {
				answer = f.Options[0].Label
				log.Warn().Str("question", f.Question).Str("fallback", f.Options[0].Label).Msg("form: checkbox fallback to first option")
			}
			var targetID string
			for _, opt := range f.Options {
				if strings.EqualFold(strings.TrimSpace(opt.Label), strings.TrimSpace(answer)) ||
					strings.Contains(strings.ToLower(opt.Label), strings.ToLower(strings.TrimSpace(answer))) {
					targetID = opt.ID
					break
				}
			}
			if targetID == "" && len(f.Options) > 0 {
				targetID = f.Options[0].ID
			}
			if targetID != "" {
				if _, err := page.Eval(jsFillCheckbox, targetID); err == nil {
					log.Info().Str("question", f.Question).Str("answer", answer).Msg("form: checkbox answered")
					filled = true
				} else {
					log.Warn().Err(err).Str("question", f.Question).Msg("form: checkbox fill failed")
				}
			}

		case "select":
			labels := f.optionLabels()
			// Open-ended prompts misclassified as <select>: ask LLM for free text,
			// then fill the sibling textarea — never force an essay into a dropdown.
			if looksLikeOpenEndedQuestion(f.Question) {
				answer := b.answerFormQuestion(ctx, lazy, f.Question, nil)
				if ok := b.fillOpenEndedNearSelect(page, f, answer); ok {
					filled = true
					break
				}
				log.Warn().Str("question", f.Question).Msg("form: open-ended question has no textarea to fill")
			}
			answer := b.answerFormQuestion(ctx, lazy, f.Question, labels)
			if answer == "" && len(f.Options) > 0 {
				answer = f.Options[0].Label
			}
			if answer != "" {
				if chosen, ok := matchSelectOption(f, answer); ok {
					_, _ = page.Eval(jsFillSelect, f.ID, chosen)
					log.Info().Str("question", f.Question).Str("answer", chosen).Msg("form: select filled")
					filled = true
				} else if ok := b.fillOpenEndedNearSelect(page, f, answer); ok {
					// LLM returned prose that doesn't match dropdown options — try textarea.
					filled = true
				} else {
					log.Warn().Str("question", f.Question).Str("answer", answer).
						Strs("options", labels).Msg("form: select answer did not match any option")
				}
			}

		case "text":
			answer := b.answerFormQuestion(ctx, lazy, f.Question, nil)
			if answer == "" {
				continue
			}
			if f.InputType == "numeric" {
				answer = extractFirstNumber(answer)
			}

			if f.Typeahead {
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
				log.Info().Str("question", f.Question).Str("answer", answer).Msg("form: typeahead typed via CDP")

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
							log.Info().Str("question", f.Question).Str("answer", answer).Msg("form: typeahead confirmed via ArrowDown+Enter fallback")
							confirmed = true
						}
						break
					}
					time.Sleep(400 * time.Millisecond)
				}
				if !confirmed {
					if dbg, err := page.Eval(jsTypeaheadDebug); err == nil {
						log.Warn().Str("dom", dbg.Value.String()).Str("question", f.Question).Msg("form: typeahead dropdown not detected")
					} else {
						log.Warn().Str("question", f.Question).Msg("form: typeahead dropdown not detected")
					}
				}
				if confirmed {
					filled = true
				}
				continue
			}

			// Plain text field, React value setter is fine.
			_, _ = page.Eval(jsFillText, f.ID, f.Index, answer)
			log.Info().Str("question", f.Question).Str("answer", answer).Str("inputType", f.InputType).Msg("form: text filled")
			filled = true
		}
	}

	if filled {
		time.Sleep(600 * time.Millisecond) // let React process field changes
	}
	return filled, true
}

// formUploadFile uploads filePath into the nth file input, walking shadow DOM to find it.
func (b *Bot) formUploadFile(page *rod.Page, filePath string, index int) error {
	// Mark the target file input via JS (handles shadow DOM).
	res, err := page.Eval(jsFillFileNth, index)
	if err != nil || res.Value.Nil() {
		return fmt.Errorf("file input at index %d not found in shadow DOM", index)
	}
	// Now find it via the marker attribute rod can locate.
	el, err := page.ElementByJS(rod.Eval(`() => {
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
		return allInDOM(document, 'input[type="file"][data-rod-upload-target]')[0] || null;
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

// ── answerFormQuestion ────────────────────────────────────────────────────────

// answerFormQuestion returns the best answer for a form field.
// options is non-nil for radio/select, the returned string should match one
// of the option labels.  For free-text fields options is nil.
//
// Strategy: heuristics only for the two clear binary cases (visa/sponsorship and
// work-authorisation) where the profile has a definitive yes/no.  Everything else
// goes straight to the LLM so it can reason over the full profile context and
// handle any question dynamically.
func (b *Bot) answerFormQuestion(ctx context.Context, lazy *lazyDocGen, question string, options []string) string {
	lower := strings.ToLower(question)
	ad := b.profileDefaults()

	// ── Fast heuristics, binary yes/no only ──────────────────────────────────

	// Sponsorship / visa
	if containsAny(lower, "sponsor", "visa status", "work permit", "require.*visa", "need.*sponsor") {
		if ad.RequiresSponsorship {
			ans := pickFormOption(options, "yes")
			if ans == "" {
				ans = "yes"
			}
			log.Info().Str("question", question).Str("answer", ans).Msg("form: answered via heuristic (sponsorship)")
			return ans
		}
		ans := pickFormOption(options, "no")
		if ans == "" {
			ans = "no"
		}
		log.Info().Str("question", question).Str("answer", ans).Msg("form: answered via heuristic (sponsorship)")
		return ans
	}

	// Work authorisation
	if containsAny(lower, "authoris", "authoriz", "legally permitted", "eligible to work", "right to work", "legally able", "work without") {
		if ad.RequiresSponsorship {
			ans := pickFormOption(options, "no")
			if ans == "" {
				ans = "no"
			}
			log.Info().Str("question", question).Str("answer", ans).Msg("form: answered via heuristic (work auth)")
			return ans
		}
		ans := pickFormOption(options, "yes")
		if ans == "" {
			ans = "yes"
		}
		log.Info().Str("question", question).Str("answer", ans).Msg("form: answered via heuristic (work auth)")
		return ans
	}

	// ── LLM, handles all other questions dynamically ─────────────────────────
	if b.cfg.Tailor == nil || lazy == nil {
		log.Debug().Bool("tailor_nil", b.cfg.Tailor == nil).Bool("lazy_nil", lazy == nil).
			Str("question", question).Msg("form: LLM skipped (not configured)")
	} else {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		answer, err := b.cfg.Tailor.AnswerFormQuestion(llmCtx, lazy.formProfileJSON(), question, options)
		if err == nil && answer != "" {
			log.Info().Str("question", question).Str("answer", answer).Msg("form: answered via LLM")
			return answer
		}
		if err != nil {
			log.Warn().Err(err).Str("question", question).Msg("form: LLM answer failed")
		} else {
			log.Warn().Str("question", question).Msg("form: LLM returned empty answer")
		}
	}

	// ── Last resort: first option or empty ────────────────────────────────────
	if len(options) > 0 {
		log.Warn().Str("question", question).Str("fallback", options[0]).Msg("form: answered via last-resort fallback")
		return options[0]
	}
	return ""
}

// profileDefaults extracts the application-defaults section from the bot's profile.
type applDefaults struct {
	RequiresSponsorship bool
	NoticePeriod        string
	SalaryExpectation   string
}

func (b *Bot) profileDefaults() applDefaults {
	if b.cfg.Profile == nil {
		return applDefaults{}
	}
	ad := b.cfg.Profile.ApplicationDefaults
	return applDefaults{
		RequiresSponsorship: ad.RequiresSponsorship,
		NoticePeriod:        ad.NoticePeriod,
		SalaryExpectation:   ad.SalaryExpectation,
	}
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
	lower := strings.ToLower(answer)
	for _, o := range f.Options {
		if strings.EqualFold(strings.TrimSpace(o.Label), answer) || strings.EqualFold(strings.TrimSpace(o.Value), answer) {
			return o.Value, true
		}
	}
	// Numeric year answers: "8" matches "8 years" / "8+".
	num := extractFirstNumber(answer)
	if num != "" {
		for _, o := range f.Options {
			ol := strings.ToLower(o.Label)
			if ol == num || strings.HasPrefix(ol, num+" ") || strings.HasPrefix(ol, num+"+") ||
				strings.Contains(ol, " "+num+" ") || strings.HasPrefix(strings.TrimSpace(o.Value), num) {
				return o.Value, true
			}
		}
	}
	for _, o := range f.Options {
		ol := strings.ToLower(o.Label)
		if strings.Contains(ol, lower) || strings.Contains(lower, ol) {
			return o.Value, true
		}
	}
	// Essays never match a short dropdown — reject so we don't pretend success.
	if len(answer) > 40 {
		return "", false
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
// Falls back to the first option when no match is found.
func pickFormOption(options []string, needle string) string {
	if options == nil {
		return needle
	}
	lowerNeedle := strings.ToLower(needle)
	for _, o := range options {
		if strings.Contains(strings.ToLower(o), lowerNeedle) {
			return o
		}
	}
	if len(options) > 0 {
		return options[0]
	}
	return needle
}
