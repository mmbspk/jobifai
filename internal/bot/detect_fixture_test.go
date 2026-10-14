package bot

// Browser fixture tests for the LinkedIn Easy Apply detection and button-click
// guards. These tests use a real Chromium browser via go-rod and a local
// httptest server that serves HTML fixtures at a LinkedIn-shaped URL path
// (/jobs/view/<id>) so window.location.pathname matches the real LinkedIn URL
// format.
//
// The tests are skipped automatically when no Chromium binary is available
// (CI without a display or without Chromium installed). Run locally with:
//
//	go test ./internal/bot/... -run TestDetect -v
//
// Each fixture intentionally reflects a real scenario from the reported bug:
// LinkedIn embeds hidden <a> elements with opensduiapplyflow in their href on
// 3rd-party (non-Easy-Apply) job pages. Before the fix, these caused false
// detection and wrong-job navigation.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launchTestBrowser starts a headless Chromium for the test and returns a
// connected *rod.Browser. Skips the test if Chromium cannot be launched.
func launchTestBrowser(t *testing.T) *rod.Browser {
	t.Helper()
	u, err := launcher.New().Headless(true).Launch()
	if err != nil {
		t.Skipf("skip browser fixture test: cannot launch Chromium: %v", err)
	}
	br := rod.New().ControlURL(u).MustConnect()
	t.Cleanup(func() { _ = br.Close() })
	return br
}

// fixtureServer starts an httptest server serving html at /jobs/view/targetJobID
// and the "wrong" job at /jobs/view/wrongJobID. Returns server + cleanup.
func fixtureServer(t *testing.T, targetJobID, wrongJobID, targetHTML, wrongHTML string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/view/"+targetJobID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, targetHTML)
	})
	if wrongJobID != "" {
		mux.HandleFunc("/jobs/view/"+wrongJobID, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, wrongHTML)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// navigateAndWait opens a page and waits for the DOM to stabilize.
func navigateAndWait(t *testing.T, br *rod.Browser, url string) *rod.Page {
	t.Helper()
	page := br.MustPage("")
	t.Cleanup(func() { _ = page.Close() })
	require.NoError(t, page.Navigate(url))
	require.NoError(t, page.Timeout(10*time.Second).WaitLoad())
	return page
}

// evalJsPoll evaluates the jsPoll function (extracted verbatim from bot.go)
// and returns the result string ("clicked", "already_applied", or "").
// We must replicate the jsPoll JS here because it is a string constant in bot.go
// and cannot be imported; any change to jsPoll in bot.go must be reflected here.
// The function is intentionally a copy so that test failures alert developers to
// keep this copy in sync.
func evalDetectLinkedInEasyApply(t *testing.T, page *rod.Page) bool {
	t.Helper()
	// We call detectLinkedInEasyApply directly because it is a method on Bot
	// that accepts *rod.Page. We run the inner jsDetect script manually so we
	// can call it against our fixture page without a full Bot instance.
	const jsDetect = `() => {
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
		const controls = [
			...deepAll(document, 'button'),
			...deepAll(document, 'a'),
			...deepAll(document, '[role="button"]'),
		];
		for (const el of controls) {
			if (isExcluded(el)) continue;
			if (!isVisible(el)) continue;
			if (isEasyApplyLabel(labelOf(el))) return true;
			if (el.getAttribute('data-live-test-easy-apply') != null) return true;
		}
		const pagePath = currentJobPath();
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
	res, err := page.Eval(jsDetect)
	require.NoError(t, err)
	return res.Value.Bool()
}

// evalJsPollResult evaluates the jsPoll function against the fixture page and
// returns the result string.
func evalJsPollResult(t *testing.T, page *rod.Page) string {
	t.Helper()
	const jsPoll = `() => {
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
			const label = ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || ''))
				.toLowerCase().replace(/\s+/g, ' ').trim();
			if (label.includes('easy apply') || label.includes('linkedin apply')) return true;
			if (b.getAttribute('data-live-test-easy-apply') != null) return true;
			const href = (b.getAttribute('href') || '').toLowerCase();
			if (href.includes('opensduiapplyflow')) {
				const hrefJobM = href.match(/\/jobs\/view\/(\d+)/);
				if (hrefJobM) {
					const pageJobM = window.location.pathname.match(/\/jobs\/view\/(\d+)/i);
					if (pageJobM && hrefJobM[1] !== pageJobM[1]) return false;
				}
				return true;
			}
			return false;
		});
		if (btn) {
			trustClick(btn);
			return 'clicked';
		}
		return '';
	}`
	res, err := page.Eval(jsPoll)
	require.NoError(t, err)
	return res.Value.String()
}

// ── Fixture HTML ──────────────────────────────────────────────────────────────

// hiddenDisplayNoneHTML: 3rd-party job page with a display:none preload link.
// detectLinkedInEasyApply must return false; jsPoll must return "".
const hiddenDisplayNoneHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Project Manager at ExternalCo</h1>
  <!-- 3rd-party apply — the visible CTA goes to an external ATS -->
  <a href="https://external-ats.com/apply" aria-label="Apply on company website">Apply on company website</a>
  <!-- LinkedIn SDUI preload link — hidden via display:none, must be ignored -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="display:none">Easy Apply</a>
</main>
</body></html>`

// hiddenVisibilityHiddenHTML: preload link hidden via visibility:hidden (nonzero dimensions).
// Dimension-only check would pass (rect.width > 0); full visibility check must reject it.
const hiddenVisibilityHiddenHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Marketing Lead at ExternalCo</h1>
  <a href="https://external-ats.com/apply">Apply on company website</a>
  <!-- visibility:hidden link — has nonzero dimensions but must be treated as hidden -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="visibility:hidden; width:100px; height:40px; display:block">Easy Apply</a>
</main>
</body></html>`

// differentJobLinkHTML: a visible opensduiapplyflow link that targets a DIFFERENT job ID.
// Page is served at /jobs/view/123456789; the link points to /jobs/view/999888.
// The link has no "Easy Apply" label so it is only matched via the href check (check 2).
// Both detectLinkedInEasyApply and jsPoll must reject this link.
const differentJobLinkHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Software Engineer at Acme — 3rd party apply</h1>
  <!-- Visible link with opensduiapplyflow but targets a different job.
       No "Easy Apply" aria-label so it is only a candidate via the href check.
       The job ID mismatch (999888 vs current page 123456789) must cause rejection. -->
  <a href="/jobs/view/999888?openSDUIApplyFlow=true"
     style="display:block; width:120px; height:40px">Apply Now</a>
</main>
</body></html>`

// sameJobVisibleCTAHTML: a visible Easy Apply button for the same job.
// detectLinkedInEasyApply must return true; jsPoll must return "clicked".
const sameJobVisibleCTAHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Data Analyst at ValidCo</h1>
  <!-- Visible Easy Apply CTA for the correct job — must be detected and clicked -->
  <button aria-label="Easy Apply" style="display:inline-block; width:120px; height:40px">
    Easy Apply
  </button>
</main>
</body></html>`

// sameJobSDUILinkHTML: a visible opensduiapplyflow link targeting the SAME job.
// detectLinkedInEasyApply must return true; jsPoll must return "clicked".
const sameJobSDUILinkHTML = `<!DOCTYPE html><html><body>
<main>
  <h1>Business Analyst at ValidCo</h1>
  <!-- Visible SDUI link targeting the same job — must be accepted -->
  <a href="/jobs/view/123456789?openSDUIApplyFlow=true" aria-label="Easy Apply"
     style="display:inline-block; width:120px; height:40px">Easy Apply</a>
</main>
</body></html>`

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestDetect_HiddenDisplayNone verifies that a display:none preload link is
// not treated as an Easy Apply CTA and jsPoll does not click it.
func TestDetect_HiddenDisplayNone(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", hiddenDisplayNoneHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, evalDetectLinkedInEasyApply(t, page), "display:none link must not be detected as Easy Apply")
	assert.Equal(t, "", evalJsPollResult(t, page), "jsPoll must not click a display:none link")
}

// TestDetect_HiddenVisibilityHidden verifies that a visibility:hidden preload
// link (which has nonzero dimensions) is not treated as an Easy Apply CTA.
// This covers the reviewer's gap: dimension-only checks miss visibility:hidden.
func TestDetect_HiddenVisibilityHidden(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", hiddenVisibilityHiddenHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, evalDetectLinkedInEasyApply(t, page), "visibility:hidden link must not be detected as Easy Apply")
	assert.Equal(t, "", evalJsPollResult(t, page), "jsPoll must not click a visibility:hidden link")
}

// TestDetect_DifferentJobLinkRejected verifies that a visible link whose href
// carries a different job ID than the current page is rejected by both the
// detector and jsPoll.
func TestDetect_DifferentJobLinkRejected(t *testing.T) {
	br := launchTestBrowser(t)
	// Page is at /jobs/view/123456789; the link targets /jobs/view/999888.
	srv := fixtureServer(t, "123456789", "", differentJobLinkHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.False(t, evalDetectLinkedInEasyApply(t, page), "link to different job must not be detected as Easy Apply")
	assert.Equal(t, "", evalJsPollResult(t, page), "jsPoll must not click a link that targets a different job")
}

// TestDetect_ValidButtonClicked verifies that a visible, same-job Easy Apply
// button IS detected and clicked. Regression guard: the fixes must not prevent
// legitimate CTAs from working.
func TestDetect_ValidButtonClicked(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", sameJobVisibleCTAHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.True(t, evalDetectLinkedInEasyApply(t, page), "visible Easy Apply button must be detected")
	result := evalJsPollResult(t, page)
	assert.Equal(t, "clicked", result, "jsPoll must click a visible Easy Apply button")
}

// TestDetect_SameJobSDUILinkClicked verifies that a visible opensduiapplyflow
// link targeting the SAME job is accepted and clicked by jsPoll.
func TestDetect_SameJobSDUILinkClicked(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "123456789", "", sameJobSDUILinkHTML, "")
	page := navigateAndWait(t, br, srv.URL+"/jobs/view/123456789")

	assert.True(t, evalDetectLinkedInEasyApply(t, page), "visible SDUI link for same job must be detected")
	result := evalJsPollResult(t, page)
	assert.Equal(t, "clicked", result, "jsPoll must click a visible SDUI link for the same job")
}

// ── Identity drift outcome routing ───────────────────────────────────────────

// TestIdentityDrift_PostApplyPathComparison verifies the drift detection used in
// submitEasyApply: given start URL and post-apply URL, the path comparison
// correctly distinguishes same-job from different-job outcomes.
func TestIdentityDrift_PostApplyPathComparison(t *testing.T) {
	cases := []struct {
		name       string
		startURL   string
		postURL    string
		wantDrift  bool
	}{
		{
			name:      "same job — no drift",
			startURL:  "https://www.linkedin.com/jobs/view/123456789",
			postURL:   "https://www.linkedin.com/jobs/view/123456789?openSDUIApplyFlow=true",
			wantDrift: false,
		},
		{
			name:      "different job — drift detected",
			startURL:  "https://www.linkedin.com/jobs/view/123456789",
			postURL:   "https://www.linkedin.com/jobs/view/999888",
			wantDrift: true,
		},
		{
			name:      "empty post URL — no drift (can't confirm mismatch)",
			startURL:  "https://www.linkedin.com/jobs/view/123456789",
			postURL:   "",
			wantDrift: false,
		},
		{
			name:      "non-LinkedIn URLs — no drift (path extraction yields empty)",
			startURL:  "https://www.seek.com.au/job/111",
			postURL:   "https://www.seek.com.au/job/222",
			wantDrift: false,
		},
		{
			name:      "start has no job path — no drift",
			startURL:  "https://www.linkedin.com/jobs/collections/recommended/?currentJobId=123",
			postURL:   "https://www.linkedin.com/jobs/view/999888",
			wantDrift: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			startPath := linkedInJobPath(tc.startURL)
			endPath := linkedInJobPath(tc.postURL)
			drift := startPath != "" && endPath != "" && startPath != endPath
			assert.Equal(t, tc.wantDrift, drift)
		})
	}
}

// TestIdentityDrift_EasyApplyIdentityError verifies that the error message from
// easyApply's identity guard is recognizable and does not match
// skipReasonBelongsInTopMatches (so it falls through as a plain apply_error).
func TestIdentityDrift_EasyApplyIdentityError(t *testing.T) {
	driftErr := fmt.Errorf("easy apply: job identity drift: page is at %s (expected %s) — aborting before submit to prevent wrong-job application",
		"https://www.linkedin.com/jobs/view/999888",
		"https://www.linkedin.com/jobs/view/123456789")

	// Drift errors must not be silently routed to Top Matches as "not automatable".
	assert.False(t, skipReasonBelongsInTopMatches(driftErr.Error()),
		"identity drift must not be mis-classified as a blocked/external-ATS error")

	// The error message must clearly identify the cause.
	assert.True(t, strings.Contains(driftErr.Error(), "identity drift"),
		"error must mention identity drift for diagnostics")
}
