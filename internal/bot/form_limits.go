package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
)

// linkedInTextLimitViolation is a text/textarea field over its visible character cap.
type linkedInTextLimitViolation struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Question string `json:"question"`
	MaxChars int    `json:"maxChars"`
	ValueLen int    `json:"valueLen"`
	Invalid  bool   `json:"invalid"`
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
	function charLimitForField(el) {
		if (el.maxLength > 0) return el.maxLength;
		const fromAttr = parseInt(el.getAttribute('maxlength'), 10);
		if (fromAttr > 0) return fromAttr;
		let node = el.parentElement;
		for (let i = 0; i < 5 && node && node !== container; i++, node = node.parentElement) {
			const kids = node.children ? [...node.children] : [];
			for (const ch of kids) {
				const t = (ch.textContent || '').trim();
				if (!t || t.length > 80) continue;
				const ofM = t.match(/^(\d+)\s+of\s+(\d+)\s+character/i);
				if (ofM) return parseInt(ofM[2], 10);
				const slashM = t.match(/^(\d+)\s*\/\s*(\d+)$/);
				if (slashM) {
					const cap = parseInt(slashM[2], 10);
					if (cap > 0 && cap <= 5000) return cap;
				}
			}
		}
		return 0;
	}
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

// sanitizeSalaryFieldAnswer keeps a compact numeric salary (LinkedIn SDUI rejects prose).
func sanitizeSalaryFieldAnswer(answer string, maxChars int) string {
	if answer == "" {
		return ""
	}
	var digits strings.Builder
	for _, r := range answer {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return ""
	}
	return truncateRunes(digits.String(), maxChars)
}

func (b *Bot) answerFormQuestionWithLimit(ctx context.Context, lazy *lazyDocGen, question string, options []string, maxChars int) (string, error) {
	q := question
	if isSalaryFormQuestion(question) {
		limitNote := ""
		if maxChars > 0 {
			limitNote = fmt.Sprintf(" At most %d characters.", maxChars)
		}
		q = fmt.Sprintf("%s\n\nReply with a numeric base salary only (digits, optional commas). No words like negotiable.%s", question, limitNote)
	} else if maxChars > 0 {
		q = fmt.Sprintf("%s\n\nReply with at most %d characters total (including spaces). Use plain text only.", question, maxChars)
	}
	answer, err := b.answerFormQuestion(ctx, lazy, q, options)
	if err != nil {
		return "", err
	}
	if isSalaryFormQuestion(question) {
		answer = sanitizeSalaryFieldAnswer(answer, maxChars)
	} else if maxChars > 0 {
		answer = truncateRunes(answer, maxChars)
	}
	return answer, nil
}

func (b *Bot) linkedInTextLimitViolations(page *rod.Page, onlyInvalid bool) ([]linkedInTextLimitViolation, error) {
	args := map[string]interface{}{}
	if onlyInvalid {
		args["onlyInvalid"] = true
	}
	res, err := page.Timeout(5 * time.Second).Eval(jsLinkedInTextFieldLimits, args)
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
		if max <= 0 && v.ValueLen > 0 {
			max = v.ValueLen - 1
		}
		if max <= 0 && !v.Invalid {
			continue
		}
		if max <= 0 {
			max = 127
		}
		log.Warn().Str("question", v.Question).Int("max_chars", max).Int("value_len", v.ValueLen).Bool("invalid", v.Invalid).
			Msg("form: refilling field over character limit")
		answer, qErr := b.answerFormQuestionWithLimit(ctx, lazy, v.Question, nil, max)
		if qErr != nil {
			return filled, qErr
		}
		if answer == "" {
			continue
		}
		answer = sanitizeContactFieldAnswer(answer, v.Question, "text")
		if isSalaryFormQuestion(v.Question) {
			answer = sanitizeSalaryFieldAnswer(answer, max)
		}
		if _, err := page.Eval(jsFillText, v.ID, v.Index, answer); err == nil {
			log.Info().Str("question", v.Question).Int("max_chars", max).Str("answer", answer).Msg("form: refilled within character limit")
			filled = true
		}
	}
	return filled, nil
}
