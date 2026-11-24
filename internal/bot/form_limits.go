package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
)

// linkedInTextLimitViolation is a text/textarea field over its visible character cap.
type linkedInTextLimitViolation struct {
	ID         string `json:"id"`
	Index      int    `json:"index"`
	Question   string `json:"question"`
	MaxChars   int    `json:"maxChars"`
	ValueLen   int    `json:"valueLen"`
	Invalid    bool   `json:"invalid"`
	NativeType string `json:"nativeType"`
	InputType  string `json:"inputType"`
	MinValue   string `json:"minValue"`
	MaxValue   string `json:"maxValue"`
	StepValue  string `json:"stepValue"`
}

const jsLinkedInTextFieldLimits = `(opts) => {
` + jsLinkedInApplyContainerFn + `
	const onlyInvalid = !!(opts && opts.onlyInvalid);
	const container = resolveApplyContainer();
	if (!container) return JSON.stringify([]);
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
` + jsFormCharacterLimitFn + `

	function questionFor(el) {
		const aria = (el.getAttribute('aria-label') || '').trim();
		if (aria) return aria;
		const ids = (el.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean);
		for (const id of ids) {
			const n = document.getElementById(id);
			if (n && (n.textContent || '').trim()) return n.textContent.trim();
		}
		return el.placeholder || el.id || '';
	}
	function blockInvalid(el) {
		if (el.getAttribute('aria-invalid') === 'true') return true;
		const described = (el.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
		for (const id of described) {
			const n = document.getElementById(id);
			if (n && /Invalid input/i.test((n.textContent || '').trim())) return true;
		}
		let node = el.parentElement;
		for (let i = 0; i < 4 && node && node !== container; i++, node = node.parentElement) {
			const err = node.querySelector && node.querySelector('[id^="error-message-"]');
			if (err && /Invalid input/i.test((err.textContent || '').trim())) return true;
		}
		return false;
	}
	const out = [];
	let textIdx = 0;
	for (const el of allInDOM(container, 'input, textarea')) {
		if (el.tagName !== 'TEXTAREA') {
			const ty = (el.getAttribute('type') || 'text').toLowerCase();
			if (['hidden', 'file', 'radio', 'checkbox', 'submit', 'button', 'reset', 'image'].includes(ty)) continue;
		}
		const max = charLimitForField(el);
		const val = el.value || '';
		const fieldInvalid = blockInvalid(el);
		const over = max > 0 && val.length > max;
		if (onlyInvalid && !fieldInvalid && !over) {
			textIdx++;
			continue;
		}
		if (!over && !fieldInvalid) {
			textIdx++;
			continue;
		}
		out.push({
			id: el.id || '',
			index: textIdx,
			question: questionFor(el),
			maxChars: max,
			valueLen: val.length,
			invalid: fieldInvalid || over,
			nativeType: el.tagName === 'TEXTAREA' ? 'textarea' : el.type,
			inputType: el.type === 'number' || el.getAttribute('inputmode') === 'numeric' ? 'numeric' : (el.tagName === 'TEXTAREA' ? 'textarea' : el.type),
			minValue: el.getAttribute('min') || '', maxValue: el.getAttribute('max') || '', stepValue: el.getAttribute('step') || '',
		});
		textIdx++;
	}
	return JSON.stringify(out);
}`

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	out := string(runes[:max])
	// Avoid breaking a trailing surrogate pair edge (rare for form text).
	return strings.TrimSpace(out)
}

func isSalaryFormQuestion(question string) bool {
	l := strings.ToLower(question)
	return strings.Contains(l, "salary") || strings.Contains(l, "compensation") ||
		strings.Contains(l, "pay rate") || strings.Contains(l, "remuneration")
}

var salarySingleNumber = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)(?:\.[0-9]+)?$`)

// Normalize grouping in a single number only. Ranges, currencies, periods and
// compensation qualifiers remain intact; an overlong answer cannot be shortened
// by cutting digits or dropping conditions.
func sanitizeSalaryFieldAnswer(answer string, maxChars int) string {
	answer = strings.TrimSpace(answer)
	if salarySingleNumber.MatchString(answer) {
		answer = strings.ReplaceAll(answer, ",", "")
	}
	if maxChars > 0 && utf8.RuneCountInString(answer) > maxChars {
		return ""
	}
	return answer
}

func salaryAnswerForControl(field SnapField, answer string) (string, error) {
	answer = sanitizeSalaryFieldAnswer(answer, field.MaxChars)
	if answer == "" || (field.Type == SnapTypeNumber && !salarySingleNumber.MatchString(answer)) ||
		!bindingValueCompatible(field, "application_defaults.salary_expectation", answer) {
		return "", fmt.Errorf("%w: salary facts do not fit the declared control", errAdaptiveQuestionReview)
	}
	return answer, nil
}

func verifySalaryAssignment(page *rod.Page, id, answer string) bool {
	result, err := page.Timeout(5*time.Second).Eval(`(id, expected) => {
		const matches = [...document.querySelectorAll('input, textarea')].filter(el => el.id === id);
		if (matches.length !== 1) return false;
		const el = matches[0];
		return el.value === expected && (typeof el.checkValidity !== 'function' || el.checkValidity());
	}`, id, answer)
	return err == nil && result != nil && result.Value.Bool()
}

func (b *Bot) answerSalaryFormField(ctx context.Context, lazy *lazyDocGen, control formField) (string, error) {
	snap := captureStepSnapshot([]formField{control}, nil, string(b.cfg.Platform), 0, "")
	if lazy != nil {
		scopeJobDependentFields(&snap, lazy.job)
	}
	field := snap.Fields[0]
	profile := b.currentProfile()
	// Structured salary facts can qualify or conflict with older free text.
	// Interpret the complete current profile instead of bypassing those facts.
	if (profile == nil || profile.ApplicationDefaults.SalaryRange == nil) && field.Group == "" && field.HelpText == "" {
		if local, ok := deterministicResolve(field, profile); ok && local.Action == ActionFill && !local.Pending {
			if answer, err := salaryAnswerForControl(field, local.Value); err == nil {
				return answer, nil
			}
		}
	}
	if b.cfg.AdaptiveLLM == nil {
		return "", errAdaptiveQuestionReview
	}
	answer, err := b.groundedLegacyAnswer(ctx, lazy, field, profile)
	if err != nil {
		return "", err
	}
	return salaryAnswerForControl(field, answer)
}

func (b *Bot) answerFormQuestionWithLimit(ctx context.Context, lazy *lazyDocGen, question string, options []string, maxChars int) (string, error) {
	q := question
	if isSalaryFormQuestion(question) {
		control := formField{Type: "text", InputType: "text", Question: question, Required: true, MaxChars: maxChars}
		if len(options) > 0 {
			control.Type = "select"
			for _, option := range options {
				control.Options = append(control.Options, formFieldOption{Label: option, Value: option})
			}
		}
		return b.answerSalaryFormField(ctx, lazy, control)
	} else if maxChars > 0 {
		q = fmt.Sprintf("%s\n\nReply with at most %d characters total (including spaces). Use plain text only.", question, maxChars)
	}
	answer, err := b.answerFormQuestion(ctx, lazy, q, options)
	if err != nil {
		return "", err
	}
	if maxChars > 0 {
		answer = truncateRunes(answer, maxChars)
	}
	return answer, nil
}

func (b *Bot) linkedInTextLimitViolations(page *rod.Page, onlyInvalid bool) ([]linkedInTextLimitViolation, error) {
	args := map[string]interface{}{}
	if onlyInvalid {
		args["onlyInvalid"] = true
	}
	res, err := page.Timeout(5*time.Second).Eval(jsLinkedInTextFieldLimits, args)
	if err != nil || res.Value.Nil() {
		return nil, err
	}
	var violations []linkedInTextLimitViolation
	if err := json.Unmarshal([]byte(res.Value.String()), &violations); err != nil {
		return nil, err
	}
	return violations, nil
}

// refillLinkedInOverLimitFields re-answers and writes text fields that exceed SDUI caps or show Invalid input.
func (b *Bot) refillLinkedInOverLimitFields(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (bool, error) {
	violations, err := b.linkedInTextLimitViolations(page, true)
	if err != nil || len(violations) == 0 {
		return false, err
	}
	var filled bool
	for _, v := range violations {
		max := v.MaxChars
		salaryQuestion := isSalaryFormQuestion(v.Question)
		if !salaryQuestion && max <= 0 && v.ValueLen > 0 {
			max = v.ValueLen - 1
		}
		if max <= 0 && !v.Invalid {
			continue
		}
		if !salaryQuestion && max <= 0 {
			max = 127
		}
		log.Warn().Str("question", v.Question).Int("max_chars", max).Int("value_len", v.ValueLen).Bool("invalid", v.Invalid).
			Msg("form: refilling field over character limit")
		var answer string
		var qErr error
		if salaryQuestion {
			answer, qErr = b.answerSalaryFormField(ctx, lazy, formField{Type: "text", Question: v.Question, InputType: v.InputType, NativeType: v.NativeType, Required: true, MaxChars: max, MinValue: v.MinValue, MaxValue: v.MaxValue, StepValue: v.StepValue})
		} else {
			answer, qErr = b.answerFormQuestionWithLimit(ctx, lazy, v.Question, nil, max)
		}
		if qErr != nil {
			return filled, qErr
		}
		if answer == "" {
			continue
		}
		answer = sanitizeContactFieldAnswer(answer, v.Question, "text")
		if salaryQuestion {
			answer = sanitizeSalaryFieldAnswer(answer, max)
		}
		if result, err := page.Eval(jsFillText, v.ID, v.Index, answer); err != nil || result == nil || !result.Value.Bool() ||
			(salaryQuestion && !verifySalaryAssignment(page, v.ID, answer)) {
			return filled, fmt.Errorf("%w: repaired field was not accepted intact", errAdaptiveQuestionReview)
		} else {
			log.Info().Str("question", v.Question).Int("max_chars", max).Str("answer", answer).Msg("form: refilled within character limit")
			filled = true
		}
	}
	return filled, nil
}
