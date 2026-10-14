package bot

// jsLinkedInDetectCTA is the detection script used by detectLinkedInEasyApply.
// Exported at package level so integration tests can reason about it without
// duplicating the script. Any change here must be re-tested against fixture pages.
const jsLinkedInDetectCTA = `() => {
	function deepAll(root, selector) {
		const out = [];
		const walk = (node) => {
			if (!node || !node.querySelectorAll) return;
			try { out.push(...node.querySelectorAll(selector)); } catch (e) {}
			try {
				for (const el of node.querySelectorAll('*')) {
					if (el.shadowRoot) walk(el.shadowRoot);
				}
			} catch (e) {}
		};
		walk(root);
		return out;
	}
	function labelOf(el) {
		return ((el.getAttribute('aria-label') || '') + ' ' + (el.innerText || el.textContent || ''))
			.toLowerCase().replace(/\s+/g, ' ').trim();
	}
	function isExcluded(el) {
		return !!el.closest([
			'aside',
			'[class*="similar-job"]',
			'[class*="jobs-similar"]',
			'[class*="people-also-viewed"]',
			'[class*="scaffold-layout__aside"]',
			'[data-test-similar-jobs]',
			'.jobs-discovery',
		].join(','));
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
		} catch(e) { return false; }
	}
	function isEasyApplyLabel(t) {
		return t.includes('easy apply') || t.includes('linkedin apply');
	}
	function currentJobPath() {
		const m = window.location.pathname.match(/\/jobs\/view\/(\d+)/i);
		return m ? '/jobs/view/' + m[1] : '';
	}

	// 1) Visible Easy Apply control in the main job pane (LinkedIn's standard CTA).
	// Href destination is validated before accepting any label match — a link
	// labelled "Easy Apply" that carries a different job ID is rejected here.
	const pagePath = currentJobPath();
	const controls = [
		...deepAll(document, 'button'),
		...deepAll(document, 'a'),
		...deepAll(document, '[role="button"]'),
	];
	for (const el of controls) {
		if (isExcluded(el)) continue;
		if (!isVisible(el)) continue;
		const href = (el.getAttribute('href') || '').toLowerCase();
		const hrefJobM = href.match(/\/jobs\/view\/(\d+)/);
		if (hrefJobM && pagePath && '/jobs/view/' + hrefJobM[1] !== pagePath) continue;
		if (isEasyApplyLabel(labelOf(el))) return true;
		if (el.getAttribute('data-live-test-easy-apply') != null) return true;
	}

	// 2) LinkedIn SDUI Easy Apply flag on an in-pane control/link.
	// Requires full visibility (display + visibility:hidden + ancestor walk) —
	// hidden SDUI preload/analytics links are present on 3rd-party job pages too.
	// Also rejects links whose href explicitly targets a different job ID.
	for (const el of controls) {
		if (isExcluded(el)) continue;
		if (!isVisible(el)) continue;
		const href = (el.getAttribute('href') || '').toLowerCase();
		if (!href.includes('opensduiapplyflow')) continue;
		const hrefJobM = href.match(/\/jobs\/view\/(\d+)/);
		if (hrefJobM && pagePath && '/jobs/view/' + hrefJobM[1] !== pagePath) continue;
		return true;
	}

	return false;
}`

// jsLinkedInClickCTA is the poll-and-click script used inside easyApply.
// Exported at package level so integration tests can evaluate it against
// fixture pages without duplicating the script.
const jsLinkedInClickCTA = `() => {
	function trustClick(btn) {
		try { btn.scrollIntoView({ block: 'center' }); } catch(e) {}
		try { btn.focus(); } catch(e) {}
		const view = (btn.ownerDocument && btn.ownerDocument.defaultView) || window;
		const opts = { bubbles: true, cancelable: true, view: view };
		for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
			try {
				if (type.startsWith('pointer')) {
					btn.dispatchEvent(new PointerEvent(type, Object.assign({pointerId: 1, pointerType: 'mouse'}, opts)));
				} else {
					btn.dispatchEvent(new MouseEvent(type, opts));
				}
			} catch(e) {
				try { btn.dispatchEvent(new MouseEvent(type.replace('pointer', 'mouse'), opts)); } catch(e2) {}
			}
		}
		try { btn.click(); } catch(e) {}
	}
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
		} catch(e) { return false; }
	}
	const t = document.body.innerText.toLowerCase();
	if (t.includes('application submitted') || t.includes('application was sent')) {
		const hasAppStatus = document.querySelector('[class*="application-status"], [class*="ApplicationStatus"]');
		if (hasAppStatus || t.includes('application submitted') || t.includes('application was sent')) return 'already_applied';
	}
	const candidates = (() => {
		return allInDOM(document, 'button, a, [role="button"]').filter(b => {
			return !b.closest('aside, [class*="similar-job"], [class*="jobs-similar"], [class*="scaffold-layout__aside"], [class*="people-also-viewed"]');
		});
	})();
	const btn = candidates.find(b => {
		if (!isVisible(b)) return false;
		const href = (b.getAttribute('href') || '').toLowerCase();
		// Validate href destination before accepting any candidate — a link labelled
		// "Easy Apply" that targets a different job ID must not be clicked.
		const hrefJobM = href.match(/\/jobs\/view\/(\d+)/);
		if (hrefJobM) {
			const pageJobM = window.location.pathname.match(/\/jobs\/view\/(\d+)/i);
			if (pageJobM && hrefJobM[1] !== pageJobM[1]) return false;
		}
		const label = ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || ''))
			.toLowerCase().replace(/\s+/g, ' ').trim();
		if (label.includes('easy apply') || label.includes('linkedin apply')) return true;
		if (b.getAttribute('data-live-test-easy-apply') != null) return true;
		if (href.includes('opensduiapplyflow')) return true;
		return false;
	});
	if (btn) {
		trustClick(btn);
		return 'clicked';
	}
	return '';
}`
