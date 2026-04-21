package bot

// form.go — generic form-field detection and filling used by both LinkedIn Easy
// Apply and Seek Quick Apply.  Works by injecting JS into the live rod page to
// scan the current step, then filling each unanswered field via profile defaults,
// heuristics, or an LLM call.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
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
	Type      string            `json:"type"`      // file | radio | select | text
	Index     int               `json:"index"`     // ordinal among same type
	Name      string            `json:"name"`      // radio group name
	ID        string            `json:"id"`        // select / text element id
	Question  string            `json:"question"`  // visible label text
	Options   []formFieldOption `json:"options"`   // radio / select choices
	HasFile   bool              `json:"hasFile"`   // file already attached
	InputType string            `json:"inputType"` // text | number | textarea
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
			// Must be within the viewport (not translated off-screen).
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

	// Anchor on LinkedIn Easy Apply form-specific data attributes — VISIBLE only.
	const anchor =
		allInDOM(document, '[data-test-form-element]').find(isVisible) ||
		allInDOM(document, 'fieldset[data-test-form-builder-radio-button-form-component]').find(isVisible) ||
		allInDOM(document, '.jobs-easy-apply-content').find(isVisible) ||
		allInDOM(document, '.jobs-easy-apply-modal').find(isVisible);

	let container = document.body;
	if (anchor) {
		container = anchor.closest('form') || anchor.closest('.jobs-easy-apply-content') || anchor.parentElement || anchor;
	}

	// Use htmlFor property to handle IDs with special characters like ":" (LinkedIn React IDs)
	function labelFor(id) {
		if (!id) return null;
		return [...document.querySelectorAll('label[for]')].find(l => l.htmlFor === id) || null;
	}

	// ── Review-only steps (Work Experience / Education cards) — no inputs, skip immediately ──
	// These steps show read-only cards with Edit/Remove buttons. There are no form fields
	// to fill; the bot just needs to click Next/Review.
	if (allInDOM(document, '.jobs-easy-apply-repeatable-groupings__groupings').some(isVisible)) {
		return JSON.stringify([]);
	}

	const fields = [];

	// ── File inputs — scan globally via shadow DOM (LinkedIn hides these in shadow roots) ───
	// Filter to document uploads only (PDF/DOC/TXT) — skip image/video/etc.
	// Use isContainerVisible so we don't pick up file inputs from off-screen steps.
	allInDOM(document, 'input[type="file"]').filter(el => {
		const a = (el.accept || '').toLowerCase();
		const typeOk = !a || a.includes('pdf') || a.includes('doc') || a.includes('txt') || a.includes('rtf');
		return typeOk && isContainerVisible(el);
	}).forEach((el, i) => {
		fields.push({ type: 'file', index: i, hasFile: !!(el.files && el.files.length > 0) });
	});

	// ── Radio groups — unanswered ones whose CONTAINER is visible ────────────
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
			// Use JS property comparison — CSS selectors break on URN-style names
			// containing colons and parentheses (LinkedIn's form element names).
			all = allInDOM(container, 'input[type="radio"]').filter(x => x.name === r.name);
		} else {
			const parent = r.closest('[data-test-form-element], .artdeco-form-element, fieldset, [role="group"]') || r.parentElement?.parentElement;
			all = parent ? allInDOM(parent, 'input[type="radio"]') : [r];
		}
		// Filter to same-group members whose container is visible.
		all = all.filter(isContainerVisible);
		if (all.some(x => x.checked)) return;

		const grp = r.closest('fieldset, [role="group"], .fb-form-element, [data-test-form-element], .artdeco-form-element')
		            || r.parentElement?.parentElement;
		const legend = grp && grp.querySelector('legend, span[class*="label"], div[class*="label"], label');
		const question = legend ? legend.textContent.trim() : (r.name || r.id || '');

		const options = all.map(x => {
			const lbl = labelFor(x.id) || x.closest('label') || x.parentElement;
			return { id: x.id, value: x.value, label: (lbl ? lbl.textContent : x.value).trim() };
		});
		fields.push({ type: 'radio', name: r.name || groupKey, question, options });
	});

	// ── Native <select> — unset / at placeholder ──────────────────────────────
	allInDOM(container, 'select').filter(isVisible).forEach((sel, i) => {
		const blank = !sel.value || sel.selectedIndex <= 0
		              || (sel.options[sel.selectedIndex] && sel.options[sel.selectedIndex].disabled);
		if (!blank) return;
		const lbl = labelFor(sel.id);
		const opts = [...sel.options]
			.filter(o => !o.disabled && o.value !== '')
			.map(o => ({ value: o.value, label: o.text.trim() }));
		fields.push({ type: 'select', id: sel.id,
		              question: lbl ? lbl.textContent.trim() : '', options: opts });
	});

	// ── Text / number / tel / email / textarea that are empty ─────────────────
	// Track a per-page index so duplicate element IDs (LinkedIn reuses the same ID
	// for every text input) can be disambiguated when filling.
	let textIdx = 0;
	allInDOM(container, 'input[type="text"], input[type="number"], input[type="tel"], input[type="email"], textarea').filter(isVisible).forEach(el => {
		if (el.value.trim()) { textIdx++; return; }
		const isRequired = el.required || el.getAttribute('aria-required') === 'true';
		const lbl = labelFor(el.id)
		            || el.closest('[data-test-form-element], .artdeco-form-element, .fb-form-element')?.querySelector('label');
		if (!isRequired && !lbl) { textIdx++; return; }
		const question = lbl
			? lbl.textContent.trim()
			: (el.placeholder || el.getAttribute('aria-label') || el.getAttribute('name') || '');
		if (!question) { textIdx++; return; }
		fields.push({ type: 'text', id: el.id, index: textIdx,
		              question,
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
			}
		} catch(e) {}
		return r;
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
	target.click();
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
			}
		} catch(e) {}
		return r;
	}
	const el = allInDOM(document, 'input[type="radio"]').find(r => r.id === id);
	if (!el) return false;
	el.click();
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

// jsFillText sets a text/textarea using React-compatible native value setter.
// Uses shadow DOM walking to find the element. nthMatch selects among duplicate IDs.
const jsFillText = `(id, nthMatch, value) => {
	function allInDOM(root, selector) {
		const r = [];
		try {
			r.push(...root.querySelectorAll(selector));
			for (const el of root.querySelectorAll('*')) {
				if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
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

func extractFirstNumber(s string) string {
	if m := reNumber.FindString(s); m != "" {
		return m
	}
	return s
}

// fillFormStep scans the current form page/modal step for unanswered fields and
// fills them using profile defaults, heuristics, or LLM.
//
// Returns (filled, hasFields):
//   - filled=true  when at least one field was successfully written
//   - hasFields=true when the scan found at least one field (even if none could be filled)
func (b *Bot) fillFormStep(ctx context.Context, page *rod.Page, resumePath, coverPath string) (filled bool, hasFields bool) {
	res, err := page.Eval(jsScanFields)
	if err != nil {
		log.Debug().Err(err).Msg("form: scan fields eval failed")
		return false, false
	}

	var fields []formField
	if err := json.Unmarshal([]byte(res.Value.String()), &fields); err != nil {
		log.Debug().Err(err).Str("raw", res.Value.String()).Msg("form: parse fields failed")
		return false, false
	}
	if len(fields) == 0 {
		log.Debug().Msg("form: scan found no fields")
		return false, false
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
			path := resumePath
			if fileUploadIdx > 0 {
				path = coverPath // second file input → cover letter
			}
			if path == "" {
				fileUploadIdx++
				continue
			}
			// Always upload our generated file — replace LinkedIn's default resume.
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
			answer := b.answerFormQuestion(ctx, f.Question, f.optionLabels())
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

		case "select":
			answer := b.answerFormQuestion(ctx, f.Question, f.optionLabels())
			if answer == "" && len(f.Options) > 0 {
				answer = f.Options[0].Label
			}
			if answer != "" {
				// Prefer matching by label → resolve to value for the JS setter.
				chosen := answer
				for _, o := range f.Options {
					if strings.EqualFold(o.Label, answer) {
						chosen = o.Value
						break
					}
				}
				_, _ = page.Eval(jsFillSelect, f.ID, chosen)
				log.Info().Str("question", f.Question).Str("answer", answer).Msg("form: select filled")
				filled = true
			}

		case "text":
			answer := b.answerFormQuestion(ctx, f.Question, nil)
			if answer == "" {
				continue
			}
			// Numeric fields (LinkedIn marks them with a -numeric ID suffix) expect
			// just a number — extract the first integer/decimal from the LLM response.
			if f.InputType == "numeric" {
				answer = extractFirstNumber(answer)
			}
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
			try { r.push(...root.querySelectorAll(selector)); for (const el of root.querySelectorAll('*')) { if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector)); } } catch(e) {}
			return r;
		}
		allInDOM(document, 'input[data-rod-upload-target]').forEach(el => el.removeAttribute('data-rod-upload-target'));
	}`)
	return nil
}

// ── answerFormQuestion ────────────────────────────────────────────────────────

// answerFormQuestion returns the best answer for a form field.
// options is non-nil for radio/select — the returned string should match one
// of the option labels.  For free-text fields options is nil.
//
// Strategy: heuristics only for the two clear binary cases (visa/sponsorship and
// work-authorisation) where the profile has a definitive yes/no.  Everything else
// goes straight to the LLM so it can reason over the full profile context and
// handle any question dynamically.
func (b *Bot) answerFormQuestion(ctx context.Context, question string, options []string) string {
	lower := strings.ToLower(question)
	ad := b.profileDefaults()

	// ── Fast heuristics — binary yes/no only ──────────────────────────────────

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

	// ── LLM — handles all other questions dynamically ─────────────────────────
	if b.cfg.Tailor == nil || b.cfg.Profile == nil {
		log.Debug().Bool("tailor_nil", b.cfg.Tailor == nil).Bool("profile_nil", b.cfg.Profile == nil).
			Str("question", question).Msg("form: LLM skipped (not configured)")
	} else {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		answer, err := b.cfg.Tailor.AnswerFormQuestion(llmCtx, b.cfg.Profile, question, options)
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
