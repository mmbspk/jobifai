package bot

import (
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// jsFindApplyPrimary locates the LinkedIn Easy Apply CTA without clicking it.
const jsFindApplyPrimary = `() => {
	function allInDOM(root, sel) {
		const r = [];
		const walk = (node) => {
			if (!node) return;
			try {
				if (node.querySelectorAll) r.push(...node.querySelectorAll(sel));
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
	function isVisible(el) {
		try {
			const rect = el.getBoundingClientRect();
			if (rect.width < 2 && rect.height < 2) return false;
			const doc = el.ownerDocument || document;
			const view = doc.defaultView || window;
			let node = el;
			while (node && node !== doc.documentElement) {
				const s = view.getComputedStyle(node);
				if (s.display === 'none' || s.visibility === 'hidden') return false;
				const parent = node.parentElement;
				if (parent) { node = parent; continue; }
				const root = node.getRootNode && node.getRootNode();
				if (root && root.host) { node = root.host; continue; }
				break;
			}
			return true;
		} catch (e) { return false; }
	}
	function isEnabled(btn) {
		if (btn.disabled) return false;
		if (btn.getAttribute('aria-disabled') === 'true') return false;
		if (btn.classList && btn.classList.contains('artdeco-button--disabled')) return false;
		return true;
	}
	function btnLabel(btn) {
		return ((btn.getAttribute('aria-label') || '') + ' ' + (btn.innerText || btn.textContent || ''))
			.trim().replace(/\s+/g, ' ');
	}
	function isMessagingChrome(el) {
		if (!el) return false;
		try {
			if (el.closest && el.closest(
				'.msg-overlay-conversation-bubble, .msg-overlay-list-bubble, .msg-form, .msg-conversations-container, .msg-overlay, [data-test-messaging]'
			)) return true;
		} catch (e) {}
		const t = btnLabel(el).toLowerCase();
		return t.includes('conversation') || t.includes('messaging');
	}
	function isExcludedRail(el) {
		try {
			return !!el.closest('aside, [class*="similar-job"], [class*="jobs-similar"], [class*="scaffold-layout__aside"], [class*="people-also-viewed"]');
		} catch (e) { return false; }
	}
	function actionKind(btn) {
		const t = btnLabel(btn).toLowerCase();
		if (btn.hasAttribute('data-live-test-easy-apply-submit-button') || btn.hasAttribute('data-easy-apply-submit-button')) return 'submit';
		if (btn.hasAttribute('data-live-test-easy-apply-review-button') || btn.hasAttribute('data-easy-apply-review-btn')) return 'review';
		if (btn.hasAttribute('data-live-test-easy-apply-next-button') || btn.hasAttribute('data-easy-apply-next-button')) return 'next';
		if (t.includes('submit application') || t === 'submit' || t.startsWith('submit ')) return 'submit';
		if (t.includes('review your application') || t === 'review' || t.startsWith('review ')) return 'review';
		if (t.includes('continue to next step') || t === 'continue' || t.startsWith('continue ')) return 'continue';
		if (t === 'next' || t.startsWith('next ') || t === 'done' || t.startsWith('done ')) return 'next';
		return '';
	}
	const priority = ['submit', 'review', 'continue', 'next'];
	const candidates = allInDOM(document, 'button, [role="button"], a[role="button"]')
		.filter(b => !isMessagingChrome(b) && !isExcludedRail(b) && actionKind(b));
	for (const kind of priority) {
		const btn = candidates.find(b => actionKind(b) === kind && isVisible(b));
		if (btn) {
			return {
				found: true,
				enabled: isEnabled(btn),
				label: btnLabel(btn),
				kind: kind,
			};
		}
	}
	return { found: false, enabled: false, label: '', kind: '' };
}`

const jsClickApplyNavigation = `() => {
	function allInDOM(root, sel) {
		const r = [];
		const walk = (node) => {
			if (!node) return;
			try {
				if (node.querySelectorAll) r.push(...node.querySelectorAll(sel));
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
	function isVisible(el) {
		try {
			const rect = el.getBoundingClientRect();
			if (rect.width < 2 && rect.height < 2) return false;
			return true;
		} catch (e) { return false; }
	}
	function isEnabled(btn) {
		if (btn.disabled) return false;
		if (btn.getAttribute('aria-disabled') === 'true') return false;
		return true;
	}
	function btnLabel(btn) {
		return ((btn.getAttribute('aria-label') || '') + ' ' + (btn.innerText || btn.textContent || ''))
			.trim().replace(/\s+/g, ' ');
	}
	function actionKind(btn) {
		const t = btnLabel(btn).toLowerCase();
		if (btn.hasAttribute('data-live-test-easy-apply-submit-button') || btn.hasAttribute('data-easy-apply-submit-button')) return 'submit';
		if (btn.hasAttribute('data-live-test-easy-apply-review-button') || btn.hasAttribute('data-easy-apply-review-btn')) return 'review';
		if (btn.hasAttribute('data-live-test-easy-apply-next-button') || btn.hasAttribute('data-easy-apply-next-button')) return 'next';
		if (t.includes('submit application') || t === 'submit' || t.startsWith('submit ')) return 'submit';
		if (t.includes('review your application') || t === 'review' || t.startsWith('review ')) return 'review';
		if (t.includes('continue to next step') || t === 'continue' || t.startsWith('continue ')) return 'continue';
		if (t === 'next' || t.startsWith('next ') || t === 'done' || t.startsWith('done ')) return 'next';
		return '';
	}
	function trustClick(btn) {
		const label = btnLabel(btn);
		try { btn.scrollIntoView({ block: 'nearest', inline: 'nearest' }); } catch (e) {}
		try { btn.focus(); } catch (e) {}
		const view = (btn.ownerDocument && btn.ownerDocument.defaultView) || window;
		const opts = { bubbles: true, cancelable: true, view: view };
		for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
			try {
				if (type.startsWith('pointer')) {
					btn.dispatchEvent(new PointerEvent(type, Object.assign({ pointerId: 1, pointerType: 'mouse' }, opts)));
				} else {
					btn.dispatchEvent(new MouseEvent(type, opts));
				}
			} catch (e) {
				try { btn.dispatchEvent(new MouseEvent(type.replace('pointer', 'mouse'), opts)); } catch (e2) {}
			}
		}
		try { btn.click(); } catch (e) {}
		return { ok: true, label: label };
	}
	const navKinds = ['review', 'continue', 'next'];
	const candidates = allInDOM(document, 'button, [role="button"], a[role="button"]')
		.filter(b => navKinds.includes(actionKind(b)));
	for (const kind of navKinds) {
		const btn = candidates.find(b => actionKind(b) === kind && isVisible(b) && isEnabled(b));
		if (btn) return trustClick(btn);
	}
	return { ok: false, label: 'no navigation CTA' };
}`

// jsLinkedInAtFinalReviewFunction is shared by prepare completion, jsScanFields, and the visible-input probe.
const jsLinkedInAtFinalReviewFunction = `function linkedInAtFinalReviewPage() {
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
	function isSubmitBtn(btn) {
		const t = ((btn.getAttribute('aria-label') || '') + ' ' + (btn.textContent || '')).toLowerCase();
		if (btn.hasAttribute('data-live-test-easy-apply-submit-button') || btn.hasAttribute('data-easy-apply-submit-button')) return true;
		return t.includes('submit application') || t === 'submit' || t.startsWith('submit ');
	}
	const shells = [...document.querySelectorAll('[role="dialog"], .jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-easy-apply-modal]')];
	for (const shell of shells) {
		if (!isVisible(shell)) continue;
		const headings = [...shell.querySelectorAll('h1,h2,h3,h4,legend,[role="heading"]')].filter(isVisible);
		const hasReviewHeading = headings.some(h => {
			const t = (h.textContent || '').toLowerCase().replace(/\s+/g, ' ').trim();
			return t.includes('review') && t.includes('application');
		});
		const submitBtn = [...shell.querySelectorAll('button,[role="button"]')].find(b => isVisible(b) && isSubmitBtn(b));
		if (hasReviewHeading && submitBtn) return true;
	}
	return false;
}`

const jsLinkedInAtFinalReview = `() => {
` + jsLinkedInAtFinalReviewFunction + `
	return linkedInAtFinalReviewPage();
}`

func linkedInPrepareScanComplete(primaryKind string, atFinalReview bool) bool {
	if applyPrimaryIsSubmit(primaryKind) {
		return true
	}
	return atFinalReview
}

func (b *Bot) linkedInPrepareAtFinalReview(page *rod.Page) bool {
	tryEval := func(p *rod.Page) (bool, error) {
		res, evalErr := p.Timeout(5 * time.Second).Eval(jsLinkedInAtFinalReview)
		if evalErr != nil {
			return false, evalErr
		}
		return res.Value.Bool(), nil
	}
	ok, err := tryEval(page)
	if err == nil && ok {
		return true
	}
	frames, _ := page.Elements("iframe, frame")
	for _, fr := range frames {
		fp, ferr := fr.Frame()
		if ferr != nil || fp == nil {
			continue
		}
		if fok, ferr := tryEval(fp); ferr == nil && fok {
			return true
		}
	}
	return false
}

func (b *Bot) peekEasyApplyPrimary(page *rod.Page) (label, kind string, found, enabled bool, err error) {
	tryEval := func(p *rod.Page) (string, string, bool, bool, error) {
		res, evalErr := p.Timeout(10*time.Second).Eval(jsFindApplyPrimary)
		if evalErr != nil {
			return "", "", false, false, evalErr
		}
		v := res.Value
		return v.Get("label").String(), v.Get("kind").String(), v.Get("found").Bool(), v.Get("enabled").Bool(), nil
	}
	label, kind, found, enabled, err = tryEval(page)
	if err != nil || found {
		return label, kind, found, enabled, err
	}
	frames, frameErr := page.Elements("iframe, frame")
	if frameErr != nil {
		return label, kind, false, false, nil
	}
	for _, fr := range frames {
		fp, ferr := fr.Frame()
		if ferr != nil || fp == nil {
			continue
		}
		fl, fk, ff, fe, ferr := tryEval(fp)
		if ferr != nil {
			continue
		}
		if ff {
			return fl, fk, ff, fe, nil
		}
	}
	return label, kind, false, false, nil
}

func (b *Bot) clickEasyApplyNavigation(page *rod.Page) (label string, ok bool, err error) {
	tryEval := func(p *rod.Page) (string, bool, error) {
		res, evalErr := p.Timeout(10 * time.Second).Eval(jsClickApplyNavigation)
		if evalErr != nil {
			return "", false, evalErr
		}
		return res.Value.Get("label").String(), res.Value.Get("ok").Bool(), nil
	}
	label, ok, err = tryEval(page)
	if err != nil || ok {
		return label, ok, err
	}
	frames, _ := page.Elements("iframe, frame")
	for _, fr := range frames {
		fp, ferr := fr.Frame()
		if ferr != nil || fp == nil {
			continue
		}
		fl, fok, ferr := tryEval(fp)
		if ferr == nil && fok {
			return fl, true, nil
		}
	}
	return label, false, nil
}

func applyPrimaryIsSubmit(kind string) bool {
	return strings.EqualFold(kind, "submit")
}

func applyPrimaryAllowsPrepareNav(kind string) bool {
	switch strings.ToLower(kind) {
	case "review", "continue", "next":
		return true
	default:
		return false
	}
}
