package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
)

const applyDebugDir = "data/debug"

// saveApplyDebug writes a screenshot (and optional HTML) for diagnosing apply failures.
func saveApplyDebug(page *rod.Page, slug string) {
	if page == nil {
		return
	}
	if err := os.MkdirAll(applyDebugDir, 0o750); err != nil {
		log.Warn().Err(err).Msg("apply debug: mkdir failed")
		return
	}
	ts := time.Now().Format("20060102_His")
	base := filepath.Join(applyDebugDir, fmt.Sprintf("%s_%s", slug, ts))
	if shot, err := page.Screenshot(false, nil); err == nil {
		if werr := os.WriteFile(base+".png", shot, 0o644); werr != nil {
			log.Warn().Err(werr).Str("path", base+".png").Msg("apply debug: screenshot write failed")
		} else {
			log.Info().Str("path", base+".png").Msg("apply debug: screenshot saved")
		}
	}
	if html, err := page.HTML(); err == nil && len(html) > 0 {
		if werr := os.WriteFile(base+".html", []byte(html), 0o644); werr != nil {
			log.Warn().Err(werr).Str("path", base+".html").Msg("apply debug: html write failed")
		}
	}
}

// pageValidationErrors returns visible LinkedIn/Seek inline validation messages.
func pageValidationErrors(page *rod.Page) []string {
	if page == nil {
		return nil
	}
	res, err := page.Eval(`() => {
		const out = [];
		const push = (el) => {
			const t = (el.textContent || '').replace(/\s+/g, ' ').trim();
			if (!t || t.length >= 200) return;
			const cls = (typeof el.className === 'string' ? el.className : '');
			if (cls.includes('inline-feedback--success') || cls.includes('artdeco-inline-feedback--success')) return;
			out.push(t);
		};
		for (const el of document.querySelectorAll(
			'.artdeco-inline-feedback--error, .fb-dash-form-element__error-text, [data-test-form-element-error-message]')) {
			push(el);
		}
		for (const el of document.querySelectorAll('[role="alert"]')) {
			const cls = (typeof el.className === 'string' ? el.className : '');
			if (cls.includes('error') || cls.includes('inline-feedback--error')) {
				push(el);
			}
		}
		return [...new Set(out)];
	}`)
	if err != nil || res.Value.Nil() {
		return nil
	}
	var msgs []string
	for _, v := range res.Value.Arr() {
		if s := v.String(); s != "" {
			msgs = append(msgs, s)
		}
	}
	return filterPageValidationMessages(msgs)
}

// filterPageValidationMessages drops LinkedIn success/confirmation toasts that use alert roles.
func filterPageValidationMessages(msgs []string) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "successfully") ||
			strings.Contains(lower, "uploaded successfully") ||
			strings.HasPrefix(lower, "success") {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
