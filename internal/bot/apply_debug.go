package bot

import (
	"fmt"
	"os"
	"path/filepath"
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
		const sel = [
			'.artdeco-inline-feedback--error',
			'.fb-dash-form-element__error-text',
			'[data-test-form-element-error-message]',
			'[role="alert"]',
		];
		const out = [];
		for (const s of sel) {
			for (const el of document.querySelectorAll(s)) {
				const t = (el.textContent || '').replace(/\s+/g, ' ').trim();
				if (t && t.length < 200) out.push(t);
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
	return msgs
}
