package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/scraper"
)

func init() {
	registerRunner(domain.PlatformSeek, platformRunnerFunc(runSeek))
}

// stripInvisible removes zero-width and other invisible Unicode characters
// that Seek embeds in button labels (e.g. U+2060 Word Joiner).
func stripInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		// U+2060 WORD JOINER, U+FEFF BOM/ZWNBSP, U+200B ZERO WIDTH SPACE,
		// U+200C ZWNJ, U+200D ZWJ, U+00AD SOFT HYPHEN
		if r == 0x2060 || r == 0xFEFF || r == 0x200B || r == 0x200C || r == 0x200D || r == 0x00AD {
			return -1
		}
		return r
	}, s)
}

// ── Seek job struct ────────────────────────────────────────────────────────

type seekJob struct {
	ID             string
	Company        string
	Title          string
	Location       string
	URL            string // https://www.seek.com.au/job/<ID>
	PostedDate     string // extracted from listing card time element
	AlreadyApplied bool   // true when Seek shows an "Applied" indicator on the card
	EasyApply      bool   // true when Seek shows a Quick Apply button (Seek-hosted form)
}

// detectSeekEasyApply returns true only when the job detail page shows Seek's
// native Quick Apply CTA that jobifai can automate. External/link-out apply
// (SmartRecruiters, Workday, plain "Apply", etc.) returns false → Top Matches.
func detectSeekEasyApply(page *rod.Page) bool {
	const jsDetect = `() => {
		function labelOf(el) {
			return ((el.getAttribute('aria-label') || '') + ' ' + (el.innerText || el.textContent || ''))
				.toLowerCase().replace(/\s+/g, ' ').trim();
		}
		function isExternalHref(href) {
			if (!href) return false;
			const h = href.toLowerCase().trim();
			if (!h || h.startsWith('/') || h.includes('seek.com')) return false;
			return h.startsWith('http');
		}
		const controls = [
			...document.querySelectorAll("[data-automation='job-detail-apply']"),
			...document.querySelectorAll("[data-automation='job-detail-apply-link']"),
			...document.querySelectorAll("button[data-automation*='apply']"),
			...document.querySelectorAll("a[data-automation*='apply']"),
		];
		for (const el of controls) {
			const t = labelOf(el);
			if (!t || t.includes('applied')) continue;
			const href = el.getAttribute('href') || '';
			if (isExternalHref(href)) continue;
			if (t.includes('quick apply')) return true;
		}
		return false;
	}`
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		res, err := page.Eval(jsDetect)
		if err == nil && res.Value.Bool() {
			if html, herr := page.HTML(); herr == nil && seekDetailPageExternalApply(html) {
				return false
			}
			return true
		}
		time.Sleep(400 * time.Millisecond)
	}
	return false
}

// seekDetailPageExternalApply returns true when the job detail HTML references an
// external ATS — these may show "Quick Apply" on Seek but redirect off-site.
func seekDetailPageExternalApply(html string) bool {
	h := strings.ToLower(html)
	for _, marker := range []string{
		"smartrecruiters.com",
		"myworkdayjobs.com",
		"greenhouse.io",
		"lever.co",
		"jobadder.com",
		"icims.com",
		"taleo.net",
		"successfactors.com",
	} {
		if strings.Contains(h, marker) {
			return true
		}
	}
	return false
}



// detectSeekPageApplied returns true when the Seek job page shows that the user
// has already applied (applied label, applied-date message, or Apply button text).
func detectSeekPageApplied(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		if (document.getElementById('applied-date-message')) return true;
		if (document.querySelector(
			"[data-automation='job-detail-applied-label'], [data-automation*='applied-label'], [data-automation*='already-applied']"
		)) return true;
		const btn = document.querySelector(
			"[data-automation='job-detail-apply'], [data-automation='job-detail-apply-link'], button[data-automation*='apply']"
		);
		if (btn) {
			const t = (btn.innerText || btn.textContent || '').toLowerCase().replace(/\s+/g, ' ').trim();
			if (t === 'applied' || t.includes('you applied') || t.startsWith('applied')) return true;
		}
		return false;
	}`)
	return err == nil && res.Value.Bool()
}



// isSeekLoginPage returns true when the URL indicates Seek has redirected the
// browser to a login or OAuth page — meaning the saved session is expired.
func isSeekLoginPage(u string) bool {
	return strings.Contains(u, "/login") ||
		strings.Contains(u, "/oauth") ||
		strings.Contains(u, "sign-in")
}

// seekApplyBlockedReasonFromURL detects external ATS / bot-protection pages that
// block automated Quick Apply. These are not Seek session expiry.
func seekApplyBlockedReasonFromURL(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, "captcha-delivery.com"), strings.Contains(lu, "datadome"):
		return "bot-protection captcha (DataDome) blocked automated apply — apply manually on Seek"
	case strings.Contains(lu, "smartrecruiters.com"):
		return "SmartRecruiters-hosted Quick Apply cannot be automated — apply manually on Seek"
	case strings.Contains(lu, "myworkdayjobs.com"):
		return "Workday-hosted apply cannot be automated — apply manually on Seek"
	case strings.Contains(lu, "greenhouse.io"):
		return "Greenhouse-hosted apply cannot be automated — apply manually on Seek"
	case strings.Contains(lu, "lever.co"):
		return "Lever-hosted apply cannot be automated — apply manually on Seek"
	case strings.Contains(lu, "jobadder.com"):
		return "JobAdder-hosted apply cannot be automated — apply manually on Seek"
	case strings.HasPrefix(lu, "http") && !strings.Contains(lu, "seek.com") && !isSeekLoginPage(u):
		return "Quick Apply redirected to an external site — apply manually on Seek"
	}
	return ""
}

// seekApplyBlockedReasonFromHTML inspects page HTML when the URL alone is ambiguous
// (e.g. SmartRecruiters embedded in an iframe on captcha-delivery.com).
func seekApplyBlockedReasonFromHTML(html string) string {
	h := strings.ToLower(html)
	if strings.Contains(h, "datadome captcha") || strings.Contains(h, "captcha-delivery.com") {
		if strings.Contains(h, "smartrecruiters") {
			return "SmartRecruiters Quick Apply blocked by bot-protection captcha — apply manually on Seek"
		}
		return "bot-protection captcha blocked automated apply — apply manually on Seek"
	}
	if strings.Contains(h, "smartrecruiters.com") && strings.Contains(h, "oneclick-ui") {
		return "SmartRecruiters-hosted Quick Apply cannot be automated — apply manually on Seek"
	}
	return ""
}

func seekApplyBlockedReason(page *rod.Page) string {
	if info, err := page.Info(); err == nil {
		if r := seekApplyBlockedReasonFromURL(info.URL); r != "" {
			return r
		}
	}
	html, err := page.Timeout(3 * time.Second).HTML()
	if err != nil {
		return ""
	}
	return seekApplyBlockedReasonFromHTML(html)
}

func isSeekApplyBlockedError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "seek apply blocked:")
}

// skipReasonBelongsInTopMatches reports apply-failure skip reasons that are really
// manual-apply jobs (external ATS / captcha), not Quick Apply automation failures.
func skipReasonBelongsInTopMatches(reason string) bool {
	lower := strings.ToLower(reason)
	if !strings.HasPrefix(lower, "seek apply:") &&
		!strings.HasPrefix(lower, "easy apply:") &&
		!strings.HasPrefix(lower, "quick apply:") {
		return false
	}
	for _, marker := range []string{
		"blocked", "smartrecruiters", "external ats", "external site",
		"datadome", "captcha", "not automatable",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isSeekSessionExpiredError(err error) bool {
	if err == nil || isSeekApplyBlockedError(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "seek session expired") ||
		strings.Contains(msg, "Quick Apply redirected to login")
}

// ── Main runner ───────────────────────────────────────────────────────────

func runSeek(ctx context.Context, b *Bot) {
	br, page, err := b.launchBrowser(ctx)
	if err != nil {
		log.Error().Err(err).Msg("seek: launch browser failed")
		b.mu.Lock()
		b.state = domain.BotStateError
		b.mu.Unlock()
		return
	}
	defer br.Close()

	b.mu.Lock()
	b.seekSessionExpired = false
	b.mu.Unlock()

	b.warmSeenCache()

	// Verify session is still valid after browser launch; attempt credential recovery.
	if err := b.seekEnsureLoggedIn(page); err != nil {
		log.Error().Err(err).Msg("seek: session expired, re-login via Settings → Secrets")
		b.mu.Lock()
		b.state = domain.BotStateError
		b.mu.Unlock()
		return
	}

	limit := b.cfg.Settings.HumanBehavior.DailyApplicationLimit
	if limit == 0 {
		limit = 40
	}
	appliedToday := b.countAppliedToday()
	log.Info().Int("applied_today", appliedToday).Int("limit", limit).Msg("seek: starting, daily progress")

	// Process previously approved jobs first.
	if appliedToday < limit {
		appliedToday += b.processApprovedQueue(ctx, br, limit-appliedToday)
	}
	if b.bailIfSeekSessionExpired() {
		return
	}

	// Re-check Top Matches jobs for Quick Apply (corrects prior detection misses).
	if appliedToday < limit {
		appliedToday += b.processTopMatchesQuickApply(ctx, br, limit-appliedToday)
	}
	if b.bailIfSeekSessionExpired() {
		return
	}

	// Search every configured location target × every keyword.
	targets := b.cfg.Preferences.EffectiveSearchTargets()

	for _, target := range targets {
		locInput := target.Location
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Msgf("seek: stopped, %s", reason)
			return
		}
		if b.bailIfSeekSessionExpired() {
			return
		}
		if appliedToday >= limit {
			log.Info().Int("limit", limit).Msg("seek: stopped, daily application limit reached")
			return
		}

		resolvedLocation := ""
		if strings.TrimSpace(locInput) != "" {
			log.Info().Str("preference", locInput).Msg("seek: resolving location")
			resolvedLocation = b.resolveSeekLocation(page, locInput)
			log.Info().Str("preference", locInput).Str("resolved", resolvedLocation).
				Msg("seek: searching location")
		} else {
			log.Info().Msg("seek: searching location (nationwide)")
		}

		arrangement := searchArrangementLabel(target)
		if arrangement != "" {
			log.Info().Str("arrangement", arrangement).Msg("seek: work arrangement filter")
		}

		for _, keyword := range b.cfg.Preferences.Positions {
			if reason := b.stopReason(ctx); reason != "" {
				log.Info().Str("keyword", keyword).Msgf("seek: stopped, %s", reason)
				return
			}
			if b.bailIfSeekSessionExpired() {
				return
			}
			if appliedToday >= limit {
				log.Info().Int("limit", limit).Msg("seek: stopped, daily application limit reached")
				return
			}
			b.SetKeyword(keyword)
			n := b.processSeekKeyword(ctx, br, page, keyword, limit-appliedToday, resolvedLocation, target)
			appliedToday += n
			if b.bailIfSeekSessionExpired() {
				return
			}

			// Detect a dropped CDP connection (e.g. VPN reset) and reconnect.
			if n == 0 && isCDPDead(page) {
				log.Warn().Msg("seek: browser connection lost, attempting reconnect")
				br.Close()
				newBr, newPage, rerr := b.launchBrowser(ctx)
				if rerr != nil {
					log.Error().Err(rerr).Msg("seek: reconnect failed, stopping")
					return
				}
				br, page = newBr, newPage
				log.Info().Msg("seek: browser reconnected, retrying keyword")
				appliedToday += b.processSeekKeyword(ctx, br, page, keyword, limit-appliedToday, resolvedLocation, target)
			}
		}
	}
	b.SetKeyword("")
	log.Info().Int("applied_today", appliedToday).Int("targets", len(targets)).
		Msg("seek: stopped, all location targets and keywords processed")
}

// ── Keyword loop ──────────────────────────────────────────────────────────

func (b *Bot) processSeekKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int, resolvedLocation string, target domain.SearchTarget) int {
	jobs, err := b.scrapeSeekJobs(ctx, page, keyword, resolvedLocation, target)
	if err != nil {
		log.Error().Err(err).Str("keyword", keyword).Msg("seek: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("seek: no new jobs found for keyword")
		return 0
	}
	jobs = b.filterSeekJobs(jobs)
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("seek: all search results already known, nothing to process")
		return 0
	}
	log.Info().Msgf("seek: found %d jobs for %q, processing", len(jobs), keyword)
	applied := 0
	for _, job := range jobs {
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Str("keyword", keyword).Msgf("seek: stopped mid-keyword, %s", reason)
			return applied
		}
		if applied >= remaining {
			log.Info().Str("keyword", keyword).Int("remaining", remaining).Msg("seek: stopped mid-keyword, daily limit reached")
			return applied
		}
		b.humanPause()
		if b.processSeekJob(ctx, br, job) {
			applied++
		}
		if b.isSeekSessionExpired() {
			return applied
		}
	}
	log.Info().Str("keyword", keyword).Int("applied", applied).Msg("seek: keyword done")
	return applied
}

// filterSeekJobs drops jobs already recorded as applied, skipped, top matches, etc.
func (b *Bot) filterSeekJobs(jobs []seekJob) []seekJob {
	fresh := make([]seekJob, 0, len(jobs))
	skipped := 0
	for _, job := range jobs {
		if reason := b.alreadyAppliedReason(job.ID); reason != "" {
			skipped++
			log.Info().Msgf("seek: skip (%s): %q @ %s", reason, job.Title, job.Company)
			continue
		}
		fresh = append(fresh, job)
	}
	if skipped > 0 {
		log.Info().Int("skipped_known", skipped).Int("to_process", len(fresh)).Msg("seek: filtered known jobs from search results")
	}
	return fresh
}

// ── Job scraping ──────────────────────────────────────────────────────────

func (b *Bot) scrapeSeekJobs(ctx context.Context, page *rod.Page, keyword, resolvedLocation string, target domain.SearchTarget) ([]seekJob, error) {
	searchURL := buildSeekSearchURL(keyword, b.cfg.Preferences, target, resolvedLocation)
	log.Info().Str("keyword", keyword).Msg("seek: searching")
	log.Debug().Str("url", searchURL).Msg("seek: navigating to search URL")

	if err := page.Navigate(searchURL); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}
	_ = page.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
	log.Info().Str("keyword", keyword).Msg("seek: page loaded, scraping cards")

	cap := b.cfg.Settings.MaxJobsPerKeyword
	if cap <= 0 {
		cap = 25
	}

	seen := map[string]bool{}
	var jobs []seekJob

	for attempt := 0; len(jobs) < cap; attempt++ {
		// Hard timeouts on every CDP call. We've seen Seek's page hang
		// indefinitely on Elements()/WaitStable() when the CDP socket is
		// half-dead — the bot freezes at 0% CPU until the user kills it.
		// Bound each call so a stuck socket surfaces as an error instead.
		cards, err := page.Timeout(8 * time.Second).Elements("article[data-job-id]")
		if err != nil || len(cards) == 0 {
			cards, _ = page.Timeout(8 * time.Second).Elements("[data-testid='job-card']")
		}
		if len(cards) == 0 && attempt == 0 {
			// Check if Seek is showing a genuine zero-results page.
			if _, zrErr := page.Timeout(3 * time.Second).Element("[data-automation='search-zero-results']"); zrErr == nil {
				log.Info().Str("keyword", keyword).Msg("seek: search returned no results for keyword")
				return nil, nil
			}
			// Unexpected: page loaded but no cards and no zero-results indicator — selector may need updating.
			log.Warn().Str("keyword", keyword).Msg("seek: no job cards found, selectors may need updating")
			tp := page.Timeout(5 * time.Second)
			if html, err := tp.HTML(); err == nil {
				_ = os.WriteFile("debug_seek_page.html", []byte(html), 0o644)
				log.Warn().Msg("seek: page HTML saved to debug_seek_page.html")
			}
			if shot, err := tp.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_seek_page.png", shot, 0o644)
				log.Warn().Msg("seek: screenshot saved to debug_seek_page.png")
			}
			return nil, nil
		}

		prevCount := len(jobs)
		for _, card := range cards {
			job := extractSeekJob(card)
			if job.ID == "" || seen[job.ID] {
				continue
			}
			if !b.cfg.Preferences.ExperienceLevel.MatchesExperienceTitle(job.Title) {
				continue
			}
			seen[job.ID] = true
			jobs = append(jobs, job)
		}
		log.Info().Msgf("seek: scroll %d, found %d cards (%d new)", attempt+1, len(cards), len(jobs)-prevCount)

		// Stop if we hit the cap or no new cards appeared after scrolling.
		if len(jobs) >= cap || (attempt > 0 && len(jobs) == prevCount) {
			break
		}

		// Scroll to the last card to trigger Seek's infinite scroll observer.
		// Each scroll call is bounded — we've seen ScrollIntoView and the
		// follow-up WaitStable hang on a half-dead CDP socket too.
		if len(cards) > 0 {
			done := make(chan struct{}, 1)
			go func() {
				_ = cards[len(cards)-1].ScrollIntoView()
				done <- struct{}{}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				log.Warn().Msg("seek: scrollIntoView timed out, breaking scrape loop")
				return jobs, nil
			}
		} else {
			_, _ = page.Timeout(3 * time.Second).Eval(`() => window.scrollTo(0, document.body.scrollHeight)`)
		}
		_ = page.Timeout(3 * time.Second).WaitStable(1500 * time.Millisecond)
	}

	if len(jobs) > cap {
		jobs = jobs[:cap]
	}

	log.Info().Msgf("seek: search returned %d jobs for %q", len(jobs), keyword)
	return jobs, nil
}

func extractSeekJob(el *rod.Element) seekJob {
	var job seekJob

	// Job ID from data-job-id attribute.
	if id, err := el.Attribute("data-job-id"); err == nil && id != nil {
		job.ID = *id
	}

	// Title
	if t, err := el.Element("[data-automation='jobTitle']"); err == nil {
		job.Title, _ = t.Text()
	} else if t, err := el.Element("h3 a, h2 a"); err == nil {
		job.Title, _ = t.Text()
	}

	// Company
	if c, err := el.Element("[data-automation='jobCompany']"); err == nil {
		job.Company, _ = c.Text()
	}

	// Location
	if l, err := el.Element("[data-automation='jobLocation']"); err == nil {
		job.Location, _ = l.Text()
	} else if l, err := el.Element("[data-automation='jobCardLocation']"); err == nil {
		job.Location, _ = l.Text()
	}

	// PostedDate
	if t, err := el.Element("[data-automation='jobListingDate']"); err == nil {
		job.PostedDate, _ = t.Text()
	} else if t, err := el.Element("time[datetime]"); err == nil {
		if dt, e := t.Attribute("datetime"); e == nil && dt != nil {
			job.PostedDate = *dt
		}
	}

	// Applied indicator, Seek shows a badge on cards for jobs already applied to.
	if _, err := el.Element("[data-automation='job-card-applied-label'], [data-automation*='applied']"); err == nil {
		job.AlreadyApplied = true
	} else if txt, err := el.Text(); err == nil {
		lower := strings.ToLower(txt)
		if strings.Contains(lower, "you applied") || strings.Contains(lower, "applied on") {
			job.AlreadyApplied = true
		}
	}
	// Best-effort Quick Apply badge on listing card — detail page re-check is authoritative.
	if _, err := el.Element("[data-automation='quick-apply-label']"); err == nil {
		job.EasyApply = true
	}

	// URL, prefer canonical job URL built from ID; also try extracting from link href.
	if job.ID != "" {
		job.URL = "https://au.seek.com/job/" + job.ID
	} else if a, err := el.Element("a[href*='/job/']"); err == nil {
		if href, e := a.Attribute("href"); e == nil && href != nil {
			h := *href
			if strings.HasPrefix(h, "/") {
				h = "https://au.seek.com" + h
			}
			job.URL = strings.SplitN(h, "?", 2)[0] // drop query params
			// Attempt to extract ID from URL path /job/XXXXXX
			parts := strings.Split(job.URL, "/")
			if len(parts) > 0 && job.ID == "" {
				job.ID = parts[len(parts)-1]
			}
		}
	}

	return job
}

// ── Search URL builder ─────────────────────────────────────────────────────

func seekLocationPath(loc string) string {
	return domain.SeekLocationPath(loc)
}

func normalizeLocationKey(loc string) string {
	return domain.NormalizeLocationKey(loc)
}

// resolveSeekLocation returns a Seek ?where= value for locInput.
// Known cities use canonical formatting (no browser). Unknown strings fall back to
// autocomplete with short timeouts, then to path-based search (empty where).
func (b *Bot) resolveSeekLocation(page *rod.Page, locInput string) string {
	if where, ok := domain.SeekSearchLocation(locInput); ok {
		if where != "" {
			log.Info().Msgf("seek: resolved location %q → %q (canonical)", locInput, where)
		}
		return where
	}

	log.Info().Str("input", locInput).Msg("seek: resolving location via autocomplete")
	if err := page.Navigate("https://au.seek.com/jobs"); err != nil {
		return ""
	}
	_ = page.Timeout(15 * time.Second).WaitLoad()
	_ = page.Timeout(3 * time.Second).WaitStable(500 * time.Millisecond)

	whereEl, err := page.Timeout(5 * time.Second).Element("#SearchBar__Where")
	if err != nil {
		log.Warn().Msg("seek: location input not found on page")
		return ""
	}

	if err := whereEl.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return ""
	}
	time.Sleep(200 * time.Millisecond)

	_, _ = whereEl.Eval(`() => {
		const el = this;
		el.focus();
		el.value = '';
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
	}`)
	time.Sleep(100 * time.Millisecond)

	for _, r := range locInput {
		_ = page.InsertText(string(r))
		time.Sleep(40 * time.Millisecond)
	}

	opts, err := page.Timeout(5 * time.Second).Elements("#SearchBar__Where-menu [role='option']")
	if err != nil || len(opts) == 0 {
		opts, err = page.Timeout(2 * time.Second).Elements("[role='listbox'] [role='option']")
		if err != nil || len(opts) == 0 {
			log.Warn().Str("input", locInput).Msg("seek: location autocomplete did not appear, using path-based fallback")
			return ""
		}
	}
	var texts []string
	for _, opt := range opts {
		if text, terr := opt.Text(); terr == nil && strings.TrimSpace(text) != "" {
			texts = append(texts, strings.TrimSpace(text))
		}
	}
	best := pickBestLocationOption(locInput, texts)
	if best == "" {
		log.Warn().Str("input", locInput).Strs("options", texts).
			Msg("seek: no autocomplete option matched location, using path-based fallback")
		return ""
	}
	log.Info().Msgf("seek: resolved location %q → %q", locInput, best)
	return best
}

// pickBestLocationOption chooses the autocomplete suggestion that best matches
// the user's location preference. Returns "" when nothing is a credible match.
func pickBestLocationOption(input string, options []string) string {
	if len(options) == 0 {
		return ""
	}
	best, bestScore := "", 0
	for _, opt := range options {
		if s := scoreLocationMatch(input, opt); s > bestScore {
			bestScore = s
			best = opt
		}
	}
	// Require a real token overlap so unrelated first hits (e.g. SA for Melbourne) are rejected.
	if bestScore < 2 {
		return ""
	}
	return best
}

func scoreLocationMatch(input, option string) int {
	in := normalizeLocationKey(input)
	opt := normalizeLocationKey(option)
	if in == "" || opt == "" {
		return 0
	}
	if in == opt {
		return 100
	}
	if strings.Contains(opt, in) || strings.Contains(in, opt) {
		return 50
	}
	inTokens := strings.Fields(in)
	optTokens := strings.Fields(opt)
	score := 0
	for _, t := range inTokens {
		if t == "all" || t == "australia" {
			continue
		}
		for _, o := range optTokens {
			if t == o {
				score += 3
			} else if strings.HasPrefix(o, t) || strings.HasPrefix(t, o) {
				score++
			}
		}
	}
	return score
}

func buildSeekSearchURL(keyword string, prefs domain.WorkPreferences, target domain.SearchTarget, resolvedLocation string) string {
	params := url.Values{}
	params.Set("keywords", keyword)

	locationPath := "/jobs"
	if resolvedLocation != "" {
		params.Set("where", resolvedLocation)
	} else if loc := strings.TrimSpace(target.Location); loc != "" {
		locationPath = seekLocationPath(loc)
	} else if len(prefs.Locations) > 0 {
		locationPath = seekLocationPath(prefs.Locations[0])
	}

	// Work type (Seek codes: 242=full-time, 243=part-time, 244=contract, 245=casual)
	var worktypes []string
	if prefs.JobTypes.FullTime {
		worktypes = append(worktypes, "242")
	}
	if prefs.JobTypes.PartTime {
		worktypes = append(worktypes, "243")
	}
	if prefs.JobTypes.Contract || prefs.JobTypes.Temporary {
		worktypes = append(worktypes, "244")
	}
	if len(worktypes) > 0 {
		params.Set("worktype", strings.Join(worktypes, ","))
	}

	// Work arrangement (Seek codes: 1=onsite, 2=hybrid, 3=remote) — per search target.
	var arrangements []string
	if target.Onsite {
		arrangements = append(arrangements, "1")
	}
	if target.Hybrid {
		arrangements = append(arrangements, "2")
	}
	if target.Remote {
		arrangements = append(arrangements, "3")
	}
	if len(arrangements) > 0 {
		params.Set("workarrangement", strings.Join(arrangements, ","))
	}

	// Date range
	switch {
	case prefs.Date.Hours24:
		params.Set("daterange", "1")
	case prefs.Date.Week:
		params.Set("daterange", "7")
	case prefs.Date.Month:
		params.Set("daterange", "30")
		// AllTime: omit param
	}

	return "https://au.seek.com" + locationPath + "?" + params.Encode()
}

func searchArrangementLabel(target domain.SearchTarget) string {
	var parts []string
	if target.Onsite {
		parts = append(parts, "onsite")
	}
	if target.Hybrid {
		parts = append(parts, "hybrid")
	}
	if target.Remote {
		parts = append(parts, "remote")
	}
	return strings.Join(parts, "+")
}

// ── Per-job pipeline ──────────────────────────────────────────────────────

func (b *Bot) processSeekJob(ctx context.Context, br *rod.Browser, job seekJob) bool {
	log.Info().Msgf("seek: processing, %q @ %s", job.Title, job.Company)

	if reason := b.alreadyAppliedReason(job.ID); reason != "" {
		log.Info().Msgf("seek: skip (%s): %q @ %s", reason, job.Title, job.Company)
		return false
	}
	// Card-level applied indicator.
	if job.AlreadyApplied {
		b.recordSeekApplied(job, "", "", 0, nil)
		log.Info().Msgf("seek: skip, already applied badge on card: %q @ %s", job.Title, job.Company)
		return false
	}
	if b.isSeekJobBlacklisted(job) {
		b.recordSeekSkipped(job, "blacklisted", 0, "", nil)
		log.Info().Msgf("seek: skip, blacklisted: %q @ %s", job.Title, job.Company)
		return false
	}

	details := b.fetchSeekJobDetails(ctx, br, &job)
	if b.isSeekSessionExpired() {
		log.Error().Msgf("seek: aborting %q @ %s — session expired", job.Title, job.Company)
		return false
	}
	// Detail page may have updated AlreadyApplied (covers manual applications).
	if job.AlreadyApplied {
		b.recordSeekApplied(job, "", "", 0, nil)
		log.Info().Msgf("seek: skip, already applied (page indicator): %q @ %s", job.Title, job.Company)
		return false
	}

	llmBefore := b.llmSnapshot()
	score, reasoning, halalVerdict, ok := b.checkSeekScore(ctx, job, details.Description)
	if !ok {
		return false
	}

	if b.cfg.RequireReview {
		b.saveSeekPendingReview(ctx, &domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformSeek,
			Link:                 job.URL,
			ResumePath:           "",
			CoverLetterPath:      "",
			SuitabilityScore:     score,
			SuitabilityReasoning: reasoning,
			DueDate:              details.DueDate,
			PostedDate:           details.PostedDate,
			EasyApply:            job.EasyApply,
			HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
			CreatedAt:            time.Now(),
		})
		return false
	}

	if !job.EasyApply {
		b.saveSeekPendingReview(ctx, &domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformSeek,
			Link:                 job.URL,
			ResumePath:           "",
			CoverLetterPath:      "",
			SuitabilityScore:     score,
			SuitabilityReasoning: reasoning,
			DueDate:              details.DueDate,
			PostedDate:           details.PostedDate,
			EasyApply:            false,
			HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
			CreatedAt:            time.Now(),
		})
		log.Info().Msgf("seek: routed to Top Matches (not Quick Apply): %q @ %s", job.Title, job.Company)
		return false
	}

	// Docs generated lazily at the file-upload step, only when toggle is on.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: job.Company, Title: job.Title}, jobDesc: details.Description}
	return b.submitSeekApplication(ctx, br, job, lazy, score, reasoning, halalVerdict, llmBefore) == seekSubmitApplied
}

func (b *Bot) checkSeekScore(ctx context.Context, job seekJob, jobDesc string) (score int, reasoning string, halalVerdict []byte, ok bool) {
	minScore := b.cfg.Settings.JobSuitabilityScore
	if minScore == 0 {
		minScore = 6
	}
	if b.cfg.Scorer == nil {
		return minScore, "", nil, true
	}
	result, err := b.cfg.Scorer.EvaluateJob(ctx, b.currentProfile(), jobDesc)
	if err != nil {
		log.Warn().Err(err).Msg("seek: suitability score failed, letting job through")
		return minScore, "", nil, true
	}
	if result.Score < minScore {
		b.recordSeekSkipped(job, fmt.Sprintf("score %d < %d", result.Score, minScore), result.Score, result.Reasoning, nil)
		log.Info().Msgf("seek: skip, score %d < %d for %q @ %s", result.Score, minScore, job.Title, job.Company)
		return result.Score, result.Reasoning, nil, false
	}
	log.Info().Msgf("seek: score %d/10, %q @ %s", result.Score, job.Title, job.Company)

	// Halal check, only runs after score passes to avoid wasted LLM calls.
	// HARAM → skip; DOUBTFUL → let through but carry verdict for storage.
	if b.cfg.HalalChecker != nil {
		verdict, err := b.cfg.HalalChecker.CheckHalal(ctx, job.Title, job.Company, jobDesc)
		if err != nil {
			log.Warn().Err(err).Msg("seek: halal check failed, letting job through")
		} else if verdict.Verdict == "HARAM" {
			verdictJSON, _ := json.Marshal(verdict)
			b.recordSeekSkipped(job, "halal filter", result.Score, result.Reasoning, verdictJSON)
			log.Info().Msgf("seek: halal skip (HARAM) %q @ %s", job.Title, job.Company)
			return result.Score, result.Reasoning, nil, false
		} else if verdict.Verdict == "DOUBTFUL" {
			halalVerdict, _ = json.Marshal(verdict)
			log.Info().Msgf("seek: halal DOUBTFUL, letting through %q @ %s", job.Title, job.Company)
		}
	}

	return result.Score, result.Reasoning, halalVerdict, true
}

// fetchSeekJobDetails opens the job page via the rod browser (bypassing Seek's HTTP
// bot-detection) and extracts description, due date, posted date, and company name.
// Falls back to listing-card data when the page can't be opened or yields too little text.
func (b *Bot) fetchSeekJobDetails(ctx context.Context, br *rod.Browser, job *seekJob) scraper.JobDetails {
	page, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		log.Warn().Err(err).Str("job", job.ID).Msg("seek: open job page for details")
		return scraper.JobDetails{Description: job.Title + " at " + job.Company, PostedDate: job.PostedDate}
	}
	defer page.Close()
	page = page.Timeout(30 * time.Second)

	if err := page.WaitLoad(); err != nil {
		return scraper.JobDetails{Description: job.Title + " at " + job.Company, PostedDate: job.PostedDate}
	}
	_ = page.WaitStable(500 * time.Millisecond)

	// Log the actual URL — catches session-expired redirects to login.
	if info, err := page.Info(); err == nil {
		log.Debug().Str("url", info.URL).Msgf("seek: detail page loaded for %q", job.Title)
		if isSeekLoginPage(info.URL) {
			if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
				log.Error().Err(recoverErr).Msgf("seek: detail page redirected to login for %q — session expired", job.Title)
				b.markSeekSessionExpired()
				return scraper.JobDetails{}
			}
			_ = page.Navigate(job.URL)
			_ = page.Timeout(30 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
		}
	}
	// Defence in depth: even when the URL doesn't redirect, the SPA may render
	// the page in guest mode (no Quick Apply button). Confirm the session via
	// the same check seekApply uses before clicking Quick Apply.
	if loggedIn, err := b.seekIsLoggedIn(page); err == nil && !loggedIn {
		if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
			log.Error().Err(recoverErr).Msgf("seek: detail page rendered but not authenticated for %q — session expired", job.Title)
			b.markSeekSessionExpired()
			return scraper.JobDetails{}
		}
		// After recovery, re-navigate to the job URL so detail extraction works.
		_ = page.Navigate(job.URL)
		_ = page.Timeout(30 * time.Second).WaitLoad()
		_ = page.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
	}

	// Extract company from the detail page, more reliable than listing card selectors.
	// Use Elements() (immediate querySelectorAll) to avoid polling on non-existent selectors.
	if job.Company == "" {
		for _, sel := range []string{
			"[data-automation='advertiser-name']",
			"[data-automation='job-detail-company-name']",
			"[data-automation*='advertiser']",
			"h3[data-automation]",
		} {
			if elems, err := page.Elements(sel); err == nil && len(elems) > 0 {
				if txt, err := elems[0].Text(); err == nil && txt != "" {
					job.Company = txt
					break
				}
			}
		}
	}

	// Check detail page for applied indicator (covers manual applications not yet in our DB).
	// Elements() is immediate — no polling — so non-existent selectors return instantly.
	if !job.AlreadyApplied {
		appliedSelectors := []string{
			"[data-automation='job-detail-applied-label']",
			"[data-automation*='applied-label']",
			"[data-automation*='already-applied']",
			"#applied-date-message", // "Visited employer's application site on …"
		}
		for _, sel := range appliedSelectors {
			if elems, err := page.Elements(sel); err == nil && len(elems) > 0 {
				job.AlreadyApplied = true
				break
			}
		}
		if !job.AlreadyApplied {
			if elems, err := page.Elements("[data-automation='job-detail-apply'], button[data-automation*='apply']"); err == nil && len(elems) > 0 {
				if txt, err := elems[0].Text(); err == nil {
					lower := strings.ToLower(strings.TrimSpace(txt))
					if lower == "applied" || strings.Contains(lower, "you applied") {
						job.AlreadyApplied = true
					}
				}
			}
		}
	}

	// Authoritative Quick Apply detection on the detail page.
	job.EasyApply = detectSeekEasyApply(page)
	if job.EasyApply {
		log.Info().Msgf("seek: Quick Apply detected: %q @ %s", job.Title, job.Company)
	} else {
		log.Info().Msgf("seek: not Quick Apply, will route to Top Matches: %q @ %s", job.Title, job.Company)
	}

	rawHTML, err := page.HTML()
	if err != nil {
		return scraper.JobDetails{Description: job.Title + " at " + job.Company, PostedDate: job.PostedDate}
	}

	details := scraper.ParseHTML(rawHTML)
	if len(strings.TrimSpace(details.Description)) < 100 {
		details.Description = job.Title + " at " + job.Company
	}
	// Use listing-card date as fallback when JSON-LD is absent on the page.
	if details.PostedDate == "" {
		details.PostedDate = job.PostedDate
	}
	return details
}

func (b *Bot) generateSeekDocs(ctx context.Context, job seekJob, jobDesc string) (resumePath, coverPath string) {
	market := b.loadMarket()
	profile := b.tailoredProfile(ctx, jobDesc, market)
	if b.cfg.Renderer == nil || profile == nil {
		return
	}
	cssOverride := ""
	if market != nil && market.CSSFile != "" {
		cssOverride = market.CSSFile
	}
	if pdf, err := b.cfg.Renderer.RenderResume(ctx, profile, "", cssOverride); err == nil {
		resumePath = b.savePDF(pdf, job.Company, job.Title, "resume")
	}
	if b.cfg.Tailor != nil {
		promptCtx := jobDesc
		if market != nil && market.CoverLetterPrompt != "" {
			promptCtx = market.CoverLetterPrompt + "\n" + jobDesc
		}
		if body, err := b.cfg.Tailor.WriteCoverLetter(ctx, profile, promptCtx); err == nil {
			if pdf, err := b.cfg.Renderer.RenderCoverLetter(ctx, body, "", cssOverride); err == nil {
				coverPath = b.savePDF(pdf, job.Company, job.Title, "cover_letter")
			}
		}
	}
	return
}

func (b *Bot) saveSeekPendingReview(ctx context.Context, p *domain.PendingReview) {
	b.savePendingReview(ctx, p)
	if p.EasyApply {
		log.Info().Msgf("seek: queued for review, %q @ %s", p.Role, p.Company)
	} else {
		log.Info().Msgf("seek: added to Top Matches (manual apply), %q @ %s", p.Role, p.Company)
	}
}

// processTopMatchesQuickApply re-evaluates jobs in the Top Matches queue
// (easy_apply=0, attempt_count=0) by loading their detail pages and re-checking
// for Quick Apply. Jobs that now have Quick Apply are auto-applied; failures go to
// Cannot Apply. Jobs without Quick Apply stay in Top Matches with attempt_count
// incremented so they are not rechecked on subsequent runs.
func (b *Bot) processTopMatchesQuickApply(ctx context.Context, br *rod.Browser, remaining int) int {
	if remaining <= 0 || b.cfg.DB == nil {
		return 0
	}

	// Drain rows up-front: with SetMaxOpenConns(1) the pool only has a single
	// connection, and an open *sql.Rows pins it. Issuing UPDATE/DELETE inside
	// the loop would deadlock waiting for the connection that this same
	// goroutine is holding.
	type pendingRow struct {
		jobID, company, role, location, link string
		score                                int
		halalVerdict                         []byte
	}
	var pending []pendingRow
	{
		rows, err := b.cfg.DB.QueryContext(ctx,
			`SELECT job_id,company,role,COALESCE(location,''),link,
			        COALESCE(suitability_score,0),COALESCE(halal_verdict,'')
			 FROM jobs_pending_review
			 WHERE user_id = ? AND easy_apply = 0 AND attempt_count = 0 AND platform = ?
			 ORDER BY created_at ASC LIMIT ?`,
			b.cfg.UserID, string(domain.PlatformSeek), remaining,
		)
		if err != nil {
			log.Warn().Err(err).Msg("seek: top-matches recheck query failed")
			return 0
		}
		for rows.Next() {
			var p pendingRow
			if err := rows.Scan(&p.jobID, &p.company, &p.role, &p.location, &p.link, &p.score, &p.halalVerdict); err == nil {
				pending = append(pending, p)
			}
		}
		rows.Close()
	}

	applied := 0
	for _, p := range pending {
		if ctx.Err() != nil {
			break
		}

		page, err := br.Page(proto.TargetCreateTarget{URL: p.link})
		if err != nil {
			log.Warn().Err(err).Msgf("seek: top-matches recheck: failed to open page for %q", p.role)
			continue
		}
		_ = page.Timeout(30 * time.Second).WaitLoad()
		_ = page.WaitStable(500 * time.Millisecond)

		// Detect a dead session before doing anything destructive: if we marked
		// attempt_count and the session is invalid, we'd permanently strand a
		// real Quick Apply job in Top Matches.
		sessionDead := false
		if info, ierr := page.Info(); ierr == nil && isSeekLoginPage(info.URL) {
			sessionDead = true
		}
		if !sessionDead {
			if loggedIn, lerr := b.seekIsLoggedIn(page); lerr == nil && !loggedIn {
				sessionDead = true
			}
		}
		if sessionDead {
			log.Error().Msgf("seek: top-matches recheck: session expired for %q — aborting recheck", p.role)
			b.markSeekSessionExpired()
			_ = page.Close()
			break
		}

		// Mark as attempted once the page loads so this job isn't rechecked next run.
		appdb.ExecContextWithRetry(ctx, b.cfg.DB,
			"UPDATE jobs_pending_review SET attempt_count = attempt_count + 1 WHERE job_id = ? AND user_id = ?",
			p.jobID, b.cfg.UserID,
		)

		if !detectSeekEasyApply(page) {
			log.Info().Msgf("seek: top-matches recheck: no Quick Apply for %q @ %s, leaving in Top Matches", p.role, p.company)
			_ = page.Close()
			continue
		}
		log.Info().Msgf("seek: top-matches recheck: Quick Apply confirmed for %q @ %s, attempting apply", p.role, p.company)

		rawHTML, _ := page.HTML()
		_ = page.Close()
		jobDetails := scraper.ParseHTML(rawHTML)

		job := seekJob{ID: p.jobID, Company: p.company, Title: p.role, Location: p.location, URL: p.link, EasyApply: true}
		lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: p.company, Title: p.role}, jobDesc: jobDetails.Description}

		outcome := b.submitSeekApplication(ctx, br, job, lazy, p.score, "", p.halalVerdict, b.llmSnapshot())
		switch outcome {
		case seekSubmitApplied, seekSubmitCannotApply:
			// Applied → jobs_applied. Failed Quick Apply → Cannot Apply. Remove from Top Matches.
			appdb.ExecContextWithRetry(ctx, b.cfg.DB,
				"DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?",
				p.jobID, b.cfg.UserID,
			)
			if outcome == seekSubmitApplied {
				applied++
			}
		case seekSubmitTopMatches:
			log.Info().Msgf("seek: top-matches recheck: kept in Top Matches after non-automatable apply: %q @ %s", p.role, p.company)
		}
		if applied >= remaining {
			break
		}
	}
	return applied
}

// ── Application submission ─────────────────────────────────────────────────

// seekSubmitOutcome describes where a job lands after submitSeekApplication.
type seekSubmitOutcome int

const (
	seekSubmitApplied seekSubmitOutcome = iota
	seekSubmitCannotApply
	seekSubmitTopMatches
)

func (b *Bot) routeSeekToTopMatches(ctx context.Context, job seekJob, score int, reasoning string, halalVerdict []byte) {
	b.saveSeekPendingReview(ctx, &domain.PendingReview{
		JobID:                job.ID,
		Company:              job.Company,
		Role:                 job.Title,
		Location:             job.Location,
		Platform:             domain.PlatformSeek,
		Link:                 job.URL,
		SuitabilityScore:     score,
		SuitabilityReasoning: reasoning,
		EasyApply:            false,
		HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
		CreatedAt:            time.Now(),
	})
}

func (b *Bot) submitSeekApplication(ctx context.Context, br *rod.Browser, job seekJob, lazy *lazyDocGen, score int, reasoning string, halalVerdict []byte, llmBefore llm.UsageSnapshot) seekSubmitOutcome {
	if !job.EasyApply {
		log.Warn().Str("job", job.Title).Msg("seek: submitSeekApplication called for non-Quick Apply job — routing to Top Matches")
		b.routeSeekToTopMatches(ctx, job, score, reasoning, halalVerdict)
		return seekSubmitTopMatches
	}
	jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		log.Error().Err(err).Msg("seek: open job page")
		b.recordSeekSkipped(job, "seek apply: open job page: "+err.Error(), score, reasoning, halalVerdict)
		return seekSubmitCannotApply
	}
	defer jobPage.Close()

	if err := b.seekApply(ctx, jobPage, lazy); err != nil {
		log.Error().Err(err).Str("job", job.Title).Msg("seek: apply failed")
		if isSeekApplyBlockedError(err) {
			log.Warn().Err(err).Str("job", job.Title).Msg("seek: not automatable — routing to Top Matches")
			b.routeSeekToTopMatches(ctx, job, score, reasoning, halalVerdict)
			return seekSubmitTopMatches
		}
		if isSeekSessionExpiredError(err) {
			b.markSeekSessionExpired()
			seekDumpUploadDebug(jobPage, "stepup_auth")
			log.Error().Msg("seek: Quick Apply requires step-up authentication — please re-login interactively via Settings → Secrets, not just cookie paste")
		}
		b.recordSeekSkipped(job, "seek apply: "+err.Error(), score, reasoning, halalVerdict)
		return seekSubmitCannotApply
	}
	resume, cover := lazy.get()
	b.recordSeekApplied(job, resume, cover, score, halalVerdict)
	b.logApplied(job.Title, job.Company, domain.PlatformSeek, llmBefore)
	return seekSubmitApplied
}

// seekApply navigates Seek's Quick Apply multi-step form on the given job page.
//
// Flow:
//  1. Find the apply button and verify it's a Seek-hosted form (not external ATS)
//  2. Click apply → wait for the resume/cover-letter step to load
//  3. Upload resume PDF and cover letter PDF (activating the upload radio first)
//  4. Click Continue through any additional steps (screening questions etc.)
//  5. On the review page click Submit and verify the confirmation
func (b *Bot) seekApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (retErr error) {
	// After a successful apply, persist the post-PKCE auth0 session so that on
	// server restart the saved cookies include the freshest possible tokens.
	defer func() {
		if retErr == nil {
			b.seekPersistSession(page)
		}
	}()

	jobURL := ""
	if info, err := page.Info(); err == nil {
		jobURL = info.URL
	}

	// Only wait for load if the page hasn't already fired its load event.
	// Calling WaitLoad() on an already-loaded page blocks forever (no future event).
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("wait load: %w", err)
		}
	}
	// Give auth0-spa-js time to complete its silent re-auth via hidden iframe.
	_ = page.Timeout(5 * time.Second).WaitStable(3 * time.Second)

	// Persist the freshly-rotated refresh token so the NEXT browser launch
	// starts with a valid token (auth0 rotation means the token used to land
	// here is already consumed and must not be presented again).
	b.seekPersistSession(page)

	// Verify the session is genuinely authenticated before clicking Quick Apply.
	// Seek's SPA loads without redirecting even when auth0 silent re-auth fails
	// (it handles login_required gracefully), so a URL-only check is insufficient.
	if err := b.seekEnsureLoggedIn(page); err != nil {
		return fmt.Errorf("seek session expired — not logged in on job page, re-add your Seek session in Settings → Secrets: %w", err)
	}

	if info, err := page.Info(); err == nil {
		if isSeekLoginPage(info.URL) {
			return fmt.Errorf("seek session expired, re-login via Settings → Secrets")
		}
		// Prefer the post-recovery job URL if ensure-login navigated away.
		if jobURL == "" || isSeekLoginPage(jobURL) {
			jobURL = info.URL
		}
	}

	// Generate docs NOW, while we're still on the stable job detail page and
	// BEFORE clicking Quick Apply. The LLM call can take 60–90 s; if we click
	// first and block on lazy.get() afterwards, Seek's SPA destroys the apply
	// form (idle navigation, auth0 token rotation, etc.) and the upload step
	// runs against the wrong DOM. Blocking here means by the time we click
	// Quick Apply, the docs are ready and the upload runs on a fresh form.
	resumePath, coverPath := lazy.get()

	if err := b.seekClickQuickApply(page); err != nil {
		return err
	}
	log.Info().Msg("seek: Quick Apply button clicked, waiting for form")
	b.humanPause()

	if onLogin, err := b.seekWaitPastLogin(page); err != nil {
		return fmt.Errorf("wait form load: %w", err)
	} else if onLogin {
		// Auth0 could not silently redirect back — try stored credentials once.
		if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
			return fmt.Errorf("seek session expired — Quick Apply redirected to login, re-add your Seek session in Settings → Secrets: %w", recoverErr)
		}
		// Return to the job page and retry Quick Apply after credential recovery.
		if jobURL != "" && !isSeekLoginPage(jobURL) {
			_ = page.Navigate(jobURL)
			_ = page.Timeout(30 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
		}
		if clickErr := b.seekClickQuickApply(page); clickErr != nil {
			return fmt.Errorf("seek session expired — Quick Apply redirected to login, re-add your Seek session in Settings → Secrets: %w", clickErr)
		}
		b.humanPause()
		if stillLogin, werr := b.seekWaitPastLogin(page); werr != nil {
			return fmt.Errorf("wait form load after re-login: %w", werr)
		} else if stillLogin {
			return fmt.Errorf("seek session expired — Quick Apply redirected to login, re-add your Seek session in Settings → Secrets")
		}
	}

	// Fast-path: Seek's Quick Apply sometimes submits silently (pre-filled
	// profile) and lands directly on the post-apply success page before
	// the form is ever shown. Detect this before checking for form presence
	// so we don't misclassify it as a step-up-auth failure.
	// Require the job detail page to also show an Applied indicator — weak
	// success-page signals alone caused false "Applied ✓" in the UI.
	if b.seekIsPostApplySuccess(page) {
		log.Info().Msg("seek: possible silent Quick Apply success page — verifying on job page")
		if err := b.seekVerifyAppliedOnJobPage(page, jobURL); err != nil {
			log.Warn().Err(err).Msg("seek: success-page signal without job-page Applied confirmation")
			return fmt.Errorf("Seek showed a possible success page but the job page does not confirm the application — try again or apply manually on Seek")
		}
		log.Info().Msg("seek: Quick Apply submitted silently (pre-filled) ✓")
		return nil
	}

	if err := b.ensureSeekApplyFormVisible(page, jobURL); err != nil {
		return err
	}

	// Upload resume: activate the "Upload a resumé" radio then set file.
	if resumePath != "" {
		if err := b.seekUploadResume(page, resumePath); err != nil {
			log.Warn().Err(err).Msg("seek: resume upload failed, Seek profile resume will be used")
		}
	}

	// Upload cover letter: activate the "Upload a cover letter" radio then set file.
	if coverPath != "" {
		if err := b.seekUploadCoverLetter(page, coverPath); err != nil {
			log.Warn().Err(err).Msg("seek: cover letter upload failed, continuing without it")
		}
	}

	b.humanPause()

	// Multi-step form: click Continue/Next until we reach and complete the review page.
	for step := 0; step < 10; step++ {
		// Scroll before scan so below-the-fold consent checkboxes are visible.
		_, _ = page.Eval(`() => {
			const form = document.querySelector("[data-testid='quick-apply-form'], [data-automation='apply-form']");
			if (form) form.scrollTop = form.scrollHeight;
			window.scrollTo(0, document.body.scrollHeight);
		}`)
		time.Sleep(300 * time.Millisecond)

		// Fill any screening questions visible on the current page.
		filled, hasFields := b.fillFormStep(ctx, page, lazy)
		if hasFields && !filled {
			// React may still be mounting fields — retry once before advancing.
			time.Sleep(800 * time.Millisecond)
			filled, hasFields = b.fillFormStep(ctx, page, lazy)
		}
		if hasFields && !filled {
			if msgs := b.seekValidationMessages(page); len(msgs) > 0 {
				log.Warn().Strs("errors", msgs).Int("step", step+1).Msg("seek: validation errors with unfilled fields")
			}
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile(fmt.Sprintf("debug_seek_step_%d_unfilled.png", step+1), shot, 0o644)
			}
			return fmt.Errorf("required screening questions could not be filled at step %d", step+1)
		}
		b.humanPause()

		// Find the action button for this step — either the submit button on the
		// final review page, or a Continue/Next button on intermediate steps.
		actionBtn, isSubmit := b.seekFindActionButton(page)
		if actionBtn == nil {
			// Save a debug snapshot to help diagnose what's on the page.
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_seek_apply_stuck.png", shot, 0o644)
				log.Warn().Msg("seek: screenshot saved to debug_seek_apply_stuck.png")
			}
			return fmt.Errorf("no continue or submit button found at form step %d", step+1)
		}

		btnText, _ := actionBtn.Text()
		btnText = stripInvisible(strings.TrimSpace(btnText))

		if isSubmit {
			// Final step: ensure consent/terms checkboxes are ticked before submit.
			if b.seekEnsureConsentChecked(page) {
				if refilled, _ := b.fillFormStep(ctx, page, lazy); refilled {
					log.Info().Msg("seek: filled consent checkbox(es) before submit")
				}
			}
		}

		if err := actionBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click %q step %d: %w", btnText, step+1, err)
		}

		if isSubmit {
			log.Info().Msgf("seek: submit clicked (%q), verifying confirmation", btnText)
			if err := b.seekWaitSubmitConfirmation(page, jobURL); err != nil {
				// Retry once: consent checkbox or validation may have blocked submit.
				log.Warn().Err(err).Msg("seek: first submit unconfirmed, retrying consent+submit")
				if b.seekEnsureConsentChecked(page) {
					b.fillFormStep(ctx, page, lazy)
				}
				if retryBtn, retrySubmit := b.seekFindActionButton(page); retryBtn != nil && retrySubmit {
					if rtxt, _ := retryBtn.Text(); retryBtn.Click(proto.InputMouseButtonLeft, 1) == nil {
						log.Info().Str("btn", stripInvisible(strings.TrimSpace(rtxt))).Msg("seek: retry submit")
						if err2 := b.seekWaitSubmitConfirmation(page, jobURL); err2 == nil {
							return nil
						}
					}
				}
				return err
			}
			return nil
		}

		log.Info().Msgf("seek: form step %d, clicked %q", step+1, btnText)
		b.humanPause()
		_ = page.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
	}

	return fmt.Errorf("could not complete Quick Apply: exceeded maximum form steps")
}

// seekWaitSubmitConfirmation polls after Submit until Seek shows success UI or
// the job page confirms Applied.
func (b *Bot) seekWaitSubmitConfirmation(page *rod.Page, jobURL string) error {
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		b.humanPause()
		_ = page.Timeout(3 * time.Second).WaitStable(500 * time.Millisecond)

		if b.seekIsPostApplySuccess(page) {
			if err := b.seekVerifyAppliedOnJobPage(page, jobURL); err != nil {
				log.Warn().Err(err).Msg("seek: post-submit success UI without job-page Applied confirmation")
				return fmt.Errorf("application may not have been recorded on Seek: %w", err)
			}
			log.Info().Msg("seek: Quick Apply submitted successfully ✓")
			return nil
		}

		if msgs := b.seekValidationMessages(page); len(msgs) > 0 {
			return fmt.Errorf("submit blocked by validation: %s", strings.Join(msgs, "; "))
		}

		// Explicit success copy in headings (Seek often shows "Good luck, <name>").
		if elems, err := page.Elements("[data-automation='application-success-title'], h1, h2, [role='alert']"); err == nil {
			for _, el := range elems {
				txt, err := el.Text()
				if err != nil {
					continue
				}
				lower := strings.ToLower(txt)
				if strings.Contains(lower, "application submitted") ||
					strings.Contains(lower, "application sent") ||
					strings.Contains(lower, "successfully applied") ||
					strings.Contains(lower, "thanks for applying") ||
					strings.Contains(lower, "good luck") ||
					strings.Contains(lower, "fingers crossed") {
					log.Info().Msgf("seek: success copy on page: %q", txt)
					if err := b.seekVerifyAppliedOnJobPage(page, jobURL); err != nil {
						return fmt.Errorf("application may not have been recorded on Seek: %w", err)
					}
					log.Info().Msg("seek: Quick Apply submitted successfully ✓")
					return nil
				}
			}
		}

		time.Sleep(800 * time.Millisecond)
	}

	if info, _ := page.Info(); info != nil {
		log.Warn().Str("url", info.URL).Str("title", info.Title).Msg("seek: could not confirm submission — page state")
	}
	if shot, err := page.Screenshot(false, nil); err == nil {
		_ = os.WriteFile("debug_seek_submit_unconfirmed.png", shot, 0o644)
	}
	// Fallback: success UI may be slow/missing but Seek recorded the application.
	if jobURL != "" {
		if err := b.seekVerifyAppliedOnJobPage(page, jobURL); err == nil {
			log.Info().Msg("seek: job page confirms Applied despite missing success UI")
			return nil
		}
	}
	return fmt.Errorf("could not confirm submission")
}

// seekValidationMessages returns visible Seek form validation/error strings.
func (b *Bot) seekValidationMessages(page *rod.Page) []string {
	res, err := page.Eval(`() => {
		const out = [];
		const sels = [
			'[role="alert"]',
			'[data-automation*="error"]',
			'[data-testid*="error"]',
			'.field-error',
			'[class*="errorMessage"]',
			'[class*="ErrorMessage"]',
			'[aria-invalid="true"]',
		];
		for (const sel of sels) {
			for (const el of document.querySelectorAll(sel)) {
				const t = (el.innerText || el.textContent || el.getAttribute('aria-label') || '').replace(/\s+/g, ' ').trim();
				if (t && t.length < 200) out.push(t);
			}
		}
		const body = (document.body && document.body.innerText || '').toLowerCase();
		for (const phrase of ['this field is required', 'please answer', 'please select', 'required field', 'answer all questions']) {
			if (body.includes(phrase)) out.push(phrase);
		}
		return [...new Set(out)].slice(0, 8);
	}`)
	if err != nil {
		return nil
	}
	var msgs []string
	for _, v := range res.Value.Arr() {
		if s := strings.TrimSpace(v.String()); s != "" {
			msgs = append(msgs, s)
		}
	}
	return msgs
}

// seekWaitPastLogin waits for the page to load after a Quick Apply click.
// If the browser lands on a login page, it polls for up to 15 s to see if
// auth0 silently redirects back (it does when the session is still valid).
// Returns (true, nil) if still on a login page after the timeout — meaning
// the session is genuinely expired. Returns (false, nil) on success.
func (b *Bot) seekWaitPastLogin(page *rod.Page) (onLogin bool, err error) {
	// Quick Apply may open a SPA modal (no navigation) or redirect to login (navigation).
	// Use a short timeout: no load event in 4s means modal opened — not a login redirect.
	_ = page.Timeout(4 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)

	info, err := page.Info()
	if err != nil || !isSeekLoginPage(info.URL) {
		return false, nil // landed directly on the form
	}

	log.Debug().Msgf("seek: post-click URL: %s", info.URL)
	log.Info().Msg("seek: Quick Apply landed on login page, waiting for auth0 silent redirect (up to 15s)")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		cur, err := page.Info()
		if err != nil {
			continue
		}
		if !isSeekLoginPage(cur.URL) {
			log.Info().Msgf("seek: auth0 silently redirected to form (%s)", cur.URL)
			_ = page.Timeout(15 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
			return false, nil
		}
	}
	return true, nil // session expired — no silent redirect happened
}

// seekIsPostApplySuccess detects Seek's post-apply success/confirmation page.
// After a silent Quick Apply (pre-filled profile), Seek skips showing the form
// and lands directly on a success page. Markers must be strong: post-apply-recs
// links alone are NOT enough (they appear in other Seek contexts and caused
// false "Applied ✓" results when the job was never submitted).
func (b *Bot) seekIsPostApplySuccess(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		const scripts = [...document.querySelectorAll('script[src], link[href]')];
		const hasSuccessBundle = scripts.some(el => {
			const s = el.src || el.href || '';
			return s.includes('/jobapply/pages-Success-') || s.includes('/jobapply/pages-LinkOutSuccess-');
		});
		const hasSuccessUI = !!document.querySelector(
			"[data-automation='application-success'], [data-automation='confirmation-page'], " +
			"[data-automation='application-submitted'], [data-automation='quick-apply-success'], " +
			"[data-automation='job-applied-message'], [data-automation='application-success-title']"
		);
		const body = (document.body && document.body.innerText || '').toLowerCase();
		const hasSuccessCopy = /application (has been )?submitted|successfully applied|thanks for applying|good luck|fingers crossed/.test(body);
		const hasGoodLuck = /\bgood luck\b/.test(body);
		const hasPostApplyRecs = !!document.querySelector('a[href*="ref=post-apply-recs"]');
		if (hasSuccessUI) return true;
		if (hasSuccessBundle && (hasSuccessCopy || hasPostApplyRecs)) return true;
		if (hasSuccessCopy && hasPostApplyRecs) return true;
		if (hasGoodLuck && (hasPostApplyRecs || hasSuccessBundle)) return true;
		return false;
	}`)
	if err != nil {
		return false
	}
	return res.Value.Bool()
}

// seekVerifyAppliedOnJobPage reloads the job detail URL and requires Seek to
// show an Applied indicator. Call after any claimed Quick Apply success so we
// never record applied in JobifAI when Seek did not accept the application.
func (b *Bot) seekVerifyAppliedOnJobPage(page *rod.Page, jobURL string) error {
	if jobURL == "" || isSeekLoginPage(jobURL) {
		return fmt.Errorf("missing job URL for apply verification")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(2+attempt) * time.Second)
		}
		if err := page.Navigate(jobURL); err != nil {
			return fmt.Errorf("navigate to job for verify: %w", err)
		}
		_ = page.Timeout(30 * time.Second).WaitLoad()
		_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
		if detectSeekPageApplied(page) {
			return nil
		}
	}
	return fmt.Errorf("job page does not show Applied after Quick Apply")
}

// seekJobIDFromURL extracts the numeric Seek job id from a job or apply URL.
var reSeekJobID = regexp.MustCompile(`/job/(\d{6,})`)

func seekJobIDFromURL(raw string) string {
	if m := reSeekJobID.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

// ensureSeekApplyFormVisible polls for the Quick Apply form and runs recovery
// when Auth0 step-up or SPA routing delays form render (common on CBA jobs).
func (b *Bot) ensureSeekApplyFormVisible(page *rod.Page, jobURL string) error {
	if found, blocked := b.seekWaitForApplyForm(page, 45*time.Second); found {
		return nil
	} else if blocked != "" {
		seekDumpUploadDebug(page, "ats_blocked")
		return fmt.Errorf("seek apply blocked: %s", blocked)
	}

	curURL := ""
	if info, err := page.Info(); err == nil {
		curURL = info.URL
	}
	log.Warn().Str("url", curURL).Msg("seek: apply form not visible after click, attempting recovery")

	jobID := seekJobIDFromURL(jobURL)
	if jobID == "" {
		jobID = seekJobIDFromURL(curURL)
	}
	if jobID != "" {
		b.seekNavigateDirectApply(page, jobID)
		if found, blocked := b.seekWaitForApplyForm(page, 30*time.Second); found {
			return nil
		} else if blocked != "" {
			seekDumpUploadDebug(page, "ats_blocked")
			return fmt.Errorf("seek apply blocked: %s", blocked)
		}
	}

	// Only run credential recovery when the page is clearly logged out — not
	// when browse session is fine but apply step-up is still in progress.
	if b.seekPageNeedsLogin(page) {
		if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
			seekDumpUploadDebug(page, "stepup_no_form")
			return fmt.Errorf("seek session expired — Quick Apply requires interactive re-login via Settings → Secrets: %w", recoverErr)
		}
	}

	if jobURL != "" && !isSeekLoginPage(jobURL) {
		_ = page.Navigate(jobURL)
		_ = page.Timeout(30 * time.Second).WaitLoad()
		_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
	}

	log.Warn().Msg("seek: retrying Quick Apply click once")
	if err := b.seekClickQuickApply(page); err != nil {
		return fmt.Errorf("seek session expired — Quick Apply redirected to login (apply button vanished): %w", err)
	}
	b.humanPause()
	if onLogin, e := b.seekWaitPastLogin(page); e != nil {
		return fmt.Errorf("wait form load (retry): %w", e)
	} else if onLogin {
		if recoverErr := b.seekEnsureLoggedIn(page); recoverErr != nil {
			return fmt.Errorf("seek session expired — Quick Apply redirected to login after retry, re-add your Seek session in Settings → Secrets: %w", recoverErr)
		}
	}

	if jobID != "" {
		b.seekNavigateDirectApply(page, jobID)
	}

	if found, blocked := b.seekWaitForApplyForm(page, 45*time.Second); found {
		return nil
	} else if blocked != "" {
		seekDumpUploadDebug(page, "ats_blocked")
		return fmt.Errorf("seek apply blocked: %s", blocked)
	}

	if blocked := seekApplyBlockedReason(page); blocked != "" {
		seekDumpUploadDebug(page, "ats_blocked")
		return fmt.Errorf("seek apply blocked: %s", blocked)
	}
	if b.seekPageNeedsLogin(page) {
		seekDumpUploadDebug(page, "stepup_no_form")
		return fmt.Errorf("seek session expired — Quick Apply redirected to login (no apply form rendered after click), re-add your Seek session in Settings → Secrets")
	}

	seekDumpUploadDebug(page, "stepup_no_form")
	return fmt.Errorf("seek apply blocked: Quick Apply form did not load — try Show browser or apply manually on Seek")
}

func (b *Bot) seekNavigateDirectApply(page *rod.Page, jobID string) {
	applyURL := "https://au.seek.com/job/" + jobID + "/apply"
	log.Info().Str("job_id", jobID).Str("url", applyURL).Msg("seek: trying direct apply URL")
	if err := page.Navigate(applyURL); err != nil {
		log.Warn().Err(err).Msg("seek: direct apply navigate failed")
		return
	}
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(8 * time.Second).WaitStable(2 * time.Second)
}

func (b *Bot) seekPageNeedsLogin(page *rod.Page) bool {
	if infoIsSeekLogin(page) {
		return true
	}
	state, err := browser.PageLoginState(page, "seek")
	return err == nil && state == browser.LoginStateNo
}

// seekWaitForApplyForm polls until Quick Apply form markers appear or timeout.
// Returns blocked when an external ATS / captcha page is detected so callers
// don't misreport a valid session as expired.
func (b *Bot) seekWaitForApplyForm(page *rod.Page, timeout time.Duration) (found bool, blocked string) {
	deadline := time.Now().Add(timeout)
	lastLog := time.Time{}
	for time.Now().Before(deadline) {
		if b.seekApplyFormPresent(page) {
			log.Info().Msg("seek: Quick Apply form visible")
			return true, ""
		}
		if reason := seekApplyBlockedReason(page); reason != "" {
			log.Warn().Str("reason", reason).Msg("seek: Quick Apply blocked by external ATS or captcha")
			return false, reason
		}
		if b.seekIsPostApplySuccess(page) {
			return false, ""
		}
		if info, err := page.Info(); err == nil {
			if strings.Contains(info.URL, "nudge=apply") {
				if id := seekJobIDFromURL(info.URL); id != "" {
					b.seekNavigateDirectApply(page, id)
				}
			}
		}
		if time.Since(lastLog) >= 10*time.Second {
			cur := ""
			if info, err := page.Info(); err == nil {
				cur = info.URL
			}
			log.Info().Str("url", cur).Msg("seek: still waiting for apply form")
			lastLog = time.Now()
		}
		time.Sleep(500 * time.Millisecond)
		_ = page.Timeout(2 * time.Second).WaitStable(400 * time.Millisecond)
	}
	if b.seekApplyFormPresent(page) {
		return true, ""
	}
	if reason := seekApplyBlockedReason(page); reason != "" {
		return false, reason
	}
	return false, ""
}

// seekApplyFormPresent probes the DOM for hallmarks of Seek's Quick Apply form
// (resume/cover-letter radio inputs, the apply-form anchor, or a submit button
// inside a quick-apply container). When Seek silently redirects an apply click
// — typically the `nudge=apply:<jobId>` step-up-auth pattern that lands the
// browser on a homepage-like SPA route whose URL still contains "application"
// — the form never renders even though seekIsLoggedIn returns optimistic
// "unknown" and the URL substring guard says ok. This is the authoritative
// check: no form markers ⇒ no form, regardless of URL/login DOM.
func (b *Bot) seekApplyFormPresent(page *rod.Page) bool {
	if seekApplyFormPresentOn(page) {
		return true
	}
	if frames, err := page.Elements("iframe"); err == nil {
		for _, fr := range frames {
			fp, ferr := fr.Frame()
			if ferr != nil || fp == nil {
				continue
			}
			if seekApplyFormPresentOn(fp) {
				return true
			}
		}
	}
	return false
}

func seekApplyFormPresentOn(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		function visible(el) {
			try {
				const r = el.getBoundingClientRect();
				if (r.width < 2 && r.height < 2) return false;
				const s = getComputedStyle(el);
				return s.display !== 'none' && s.visibility !== 'hidden';
			} catch (e) { return false; }
		}
		// Strong markers — any one of these means the apply form is rendered.
		const sels = [
			"[data-testid='quick-apply-form']",
			"[data-automation='apply-form']",
			"form[data-testid*='applicationForm']",
			"input[name='resume-method']",
			"input[name='coverLetter-method']",
			"[data-testid='resumeFileInput']",
			"[data-testid='coverLetterFileInput']",
			"button[data-testid='continue-button']",
			"button[data-testid='review-submit-application']",
		];
		for (const s of sels) {
			const el = document.querySelector(s);
			if (el && visible(el)) return true;
		}
		// Visible PDF resume/cover upload inside a dialog or apply shell.
		for (const el of document.querySelectorAll('input[type="file"]')) {
			const a = (el.accept || '').toLowerCase();
			if (visible(el) && (!a || a.includes('pdf') || a.includes('doc'))) return true;
		}
		const dialog = document.querySelector('[role="dialog"]');
		if (dialog && visible(dialog)) {
			if (dialog.querySelector('button[data-testid="continue-button"], input[name="resume-method"], input[type="file"]')) {
				return true;
			}
		}
		return false;
	}`)
	if err != nil {
		return false
	}
	return res.Value.Bool()
}

// seekIsLoggedIn checks whether Seek's SPA considers the user authenticated.
// Seek loads the page even when auth0 silent re-auth fails, so a URL check alone
// is insufficient — we must probe the DOM for logged-in state indicators.
func (b *Bot) seekIsLoggedIn(page *rod.Page) (bool, error) {
	state, err := browser.PageLoginState(page, "seek")
	if err != nil {
		return true, nil // assume logged in on eval error
	}
	return state != browser.LoginStateNo, nil
}

// seekEnsureLoggedIn returns nil when Seek looks authenticated. If not, and
// stored credentials exist, it runs seekAutoLogin once and re-checks.
func (b *Bot) seekEnsureLoggedIn(page *rod.Page) error {
	state, err := browser.PageLoginState(page, "seek")
	if err == nil && state == browser.LoginStateYes {
		b.mu.Lock()
		b.seekSessionExpired = false
		b.mu.Unlock()
		return nil
	}
	if err == nil && state == browser.LoginStateUnknown {
		// Optimistic: URL/DOM inconclusive but not clearly logged out.
		if info, ierr := page.Info(); ierr == nil && !isSeekLoginPage(info.URL) {
			return nil
		}
	}

	if b.cfg.SeekEmail == "" || b.cfg.SeekPassword == "" {
		if state == browser.LoginStateNo || (err == nil && infoIsSeekLogin(page)) {
			return fmt.Errorf("not logged in and no Seek credentials saved")
		}
		if state == browser.LoginStateNo {
			return fmt.Errorf("not logged in")
		}
		return nil
	}

	// Navigate to a Seek entry point if we are not already on a login form.
	if info, ierr := page.Info(); ierr == nil && !isSeekLoginPage(info.URL) {
		_ = page.Navigate("https://au.seek.com/oauth/login")
		_ = page.Timeout(30 * time.Second).WaitLoad()
		_ = page.Timeout(5 * time.Second).WaitStable(1 * time.Second)
	}

	if autoErr := b.seekAutoLogin(page); autoErr != nil {
		return autoErr
	}

	// Land back on Seek home so Auth0 SPA can finish silent auth.
	_ = page.Navigate("https://au.seek.com/jobs")
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(8 * time.Second).WaitStable(2 * time.Second)

	state, err = browser.PageLoginState(page, "seek")
	if err == nil && state == browser.LoginStateYes {
		b.mu.Lock()
		b.seekSessionExpired = false
		b.mu.Unlock()
		b.seekPersistSession(page)
		log.Info().Msg("seek: session recovered via stored credentials")
		return nil
	}
	if info, ierr := page.Info(); ierr == nil && isSeekLoginPage(info.URL) {
		return fmt.Errorf("auto-login completed but still on login page")
	}
	if state == browser.LoginStateNo {
		return fmt.Errorf("auto-login completed but still logged out")
	}
	b.mu.Lock()
	b.seekSessionExpired = false
	b.mu.Unlock()
	b.seekPersistSession(page)
	return nil
}

func infoIsSeekLogin(page *rod.Page) bool {
	info, err := page.Info()
	return err == nil && isSeekLoginPage(info.URL)
}

// seekPersistSession snapshots all browser cookies (including login.seek.com)
// and saves them back to the session store. Called after each auth0 silent
// re-auth so the next browser launch uses the freshest refresh token.
// Skips persistence when the page is clearly logged out so a guest jar cannot
// overwrite a previously-valid session.
func (b *Bot) seekPersistSession(page *rod.Page) {
	if b.cfg.Sessions == nil {
		return
	}
	state, stateErr := browser.PageLoginState(page, "seek")
	if !browser.ShouldPersistCookies(state, stateErr) {
		log.Warn().Str("state", string(state)).Msg("seek: skipping session persist — not confirmed logged in")
		return
	}
	result, err := proto.NetworkGetAllCookies{}.Call(page)
	if err != nil {
		log.Warn().Err(err).Msg("seek: failed to read cookies for session refresh")
		return
	}
	var fresh []browser.Cookie
	for _, c := range result.Cookies {
		fresh = append(fresh, browser.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   string(c.Domain),
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   bool(c.Secure),
			SameSite: string(c.SameSite),
		})
	}
	if raw, marshalErr := browser.MarshalCookies(fresh); marshalErr != nil {
		log.Warn().Err(marshalErr).Msg("seek: failed to marshal refreshed session cookies")
	} else if err := b.cfg.Sessions.Save(b.cfg.UserID, string(b.cfg.Platform), "session", raw); err != nil {
		log.Warn().Err(err).Msg("seek: failed to refresh session cookies")
	} else {
		log.Debug().Msg("seek: session cookies refreshed after job detail page auth0 re-auth")
	}
}

// seekClickQuickApply finds the Quick Apply button, checks it's not external,
// and clicks it. Extracted so seekApply can call it twice (initial + post-login retry).
func (b *Bot) seekClickQuickApply(page *rod.Page) error {
	applyBtn, err := page.Timeout(8 * time.Second).Element("[data-automation='job-detail-apply']")
	if err != nil {
		applyBtn, err = page.Timeout(8 * time.Second).Element("a[href*='/apply'], button[data-automation*='apply']")
		if err != nil {
			return fmt.Errorf("apply button not found: %w", err)
		}
	}
	if href, e := applyBtn.Attribute("href"); e == nil && href != nil {
		h := strings.TrimSpace(*href)
		if h != "" && !strings.HasPrefix(h, "http") {
			h = "https://au.seek.com" + h
		}
		if h != "" && !strings.Contains(h, "seek.com") && strings.HasPrefix(h, "http") {
			return fmt.Errorf("external application")
		}
		// Prefer navigating to Seek-hosted apply routes — more reliable than SPA click.
		if strings.Contains(h, "seek.com") && (strings.Contains(h, "/apply") || strings.Contains(h, "application")) {
			log.Info().Str("url", h).Msg("seek: opening apply via button href")
			if err := page.Navigate(h); err != nil {
				return fmt.Errorf("navigate apply href: %w", err)
			}
			_ = page.Timeout(30 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
			return nil
		}
	}
	_, _ = applyBtn.Eval(`() => this.scrollIntoView({block:'center', inline:'center'})`)
	time.Sleep(250 * time.Millisecond)
	if err := applyBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click apply: %w", err)
	}
	return nil
}

// seekAutoLogin fills the auth0 login form at login.seek.com using stored
// credentials. Returns nil once the browser has been redirected back to seek.
func (b *Bot) seekAutoLogin(page *rod.Page) error {
	log.Info().Msg("seek: auto-login with stored credentials")

	// Wait for email field (auth0 uses name="username" for the email input).
	emailInput, err := page.Timeout(10 * time.Second).Element(
		"input[name='username'], input[type='email'], input[name='email']",
	)
	if err != nil {
		return fmt.Errorf("login form: email input not found: %w", err)
	}
	if err := emailInput.SelectAllText(); err != nil {
		_ = err
	}
	if err := emailInput.Input(b.cfg.SeekEmail); err != nil {
		return fmt.Errorf("fill email: %w", err)
	}
	b.humanPause()

	// Try to find password on same screen; if absent, click Continue first.
	pwdInput, pwdErr := page.Timeout(500 * time.Millisecond).Element("input[type='password']")
	if pwdErr != nil {
		if btn, e := page.Element("button[type='submit'], button[data-action='default']"); e == nil {
			_ = btn.Click(proto.InputMouseButtonLeft, 1)
		}
		pwdInput, pwdErr = page.Timeout(10 * time.Second).Element("input[type='password']")
		if pwdErr != nil {
			return fmt.Errorf("login form: password input not found")
		}
	}
	if err := pwdInput.Input(b.cfg.SeekPassword); err != nil {
		return fmt.Errorf("fill password: %w", err)
	}
	b.humanPause()

	if btn, e := page.Element("button[type='submit'], button[data-action='default']"); e == nil {
		_ = btn.Click(proto.InputMouseButtonLeft, 1)
	}

	_ = page.WaitLoad()
	_ = page.WaitStable(3 * time.Second)
	return nil
}

// seekFindActionButton scans the current Quick Apply page for the next action button.
// Returns (button, isSubmit): isSubmit=true when it's the final "Submit application"
// button, false when it's a Continue/Next button.
// Returns (nil, false) when neither is found.
//
// Continue is preferred over Submit when both are visible — Submit often exists in
// the DOM on intermediate steps but must not be clicked until the review page.
func (b *Bot) seekFindActionButton(page *rod.Page) (*rod.Element, bool) {
	pickVisible := func(elems rod.Elements) *rod.Element {
		for _, el := range elems {
			if b.seekButtonActionable(el) {
				return el
			}
		}
		return nil
	}

	// Continue/Next first — never skip ahead to Submit while Continue is shown.
	if elems, err := page.Elements("button[data-testid='continue-button']"); err == nil {
		if btn := pickVisible(elems); btn != nil {
			return btn, false
		}
	}

	for _, sel := range []string{
		"button[data-testid='review-submit-application']",
		"[data-automation='review-submit-button']",
		"button[data-testid='review-submit-button']",
		"button[data-testid='submit-application']",
	} {
		if elems, err := page.Elements(sel); err == nil {
			if btn := pickVisible(elems); btn != nil {
				return btn, true
			}
		}
	}

	// Text-based fallback: Continue before Submit.
	if btns, err := page.Elements("button"); err == nil {
		for _, btn := range btns {
			if !b.seekButtonActionable(btn) {
				continue
			}
			txt, err := btn.Text()
			if err != nil {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(txt))
			if strings.Contains(lower, "continue") || strings.Contains(lower, "next") {
				return btn, false
			}
		}
		for _, btn := range btns {
			if !b.seekButtonActionable(btn) {
				continue
			}
			txt, err := btn.Text()
			if err != nil {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(txt))
			if strings.Contains(lower, "submit") {
				return btn, true
			}
		}
	}
	return nil, false
}

// seekEnsureConsentChecked ticks any unchecked consent/terms checkboxes in the
// Quick Apply form. Returns true when at least one box was checked.
func (b *Bot) seekEnsureConsentChecked(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		function applyRoot() {
			return document.querySelector("[data-testid='quick-apply-form'], [data-automation='apply-form'], form[data-testid*='applicationForm']")
				|| document.body;
		}
		function isVisible(el) {
			try {
				const r = el.getBoundingClientRect();
				if (r.width < 2 && r.height < 2) return false;
				const s = getComputedStyle(el);
				return s.display !== 'none' && s.visibility !== 'hidden';
			} catch (e) { return false; }
		}
		const root = applyRoot();
		let checked = 0;
		for (const cb of root.querySelectorAll('input[type="checkbox"]')) {
			if (!isVisible(cb) || cb.checked) continue;
			const label = ((cb.closest('label') && cb.closest('label').innerText) || cb.getAttribute('aria-label') || '').toLowerCase();
			const near = (cb.closest('fieldset, div, label') && cb.closest('fieldset, div, label').innerText || '').toLowerCase();
			const blob = label + ' ' + near;
			// Skip marketing/newsletter opt-ins; tick legal/consent boxes.
			if (blob.includes('newsletter') || blob.includes('marketing') || blob.includes('job alert')) continue;
			try { cb.click(); cb.dispatchEvent(new Event('change', { bubbles: true })); checked++; } catch (e) {}
		}
		for (const w of root.querySelectorAll('[role="checkbox"]')) {
			if (!isVisible(w) || w.getAttribute('aria-checked') === 'true') continue;
			try { w.click(); checked++; } catch (e) {}
		}
		return checked;
	}`)
	return err == nil && res.Value.Int() > 0
}

func (b *Bot) seekButtonActionable(btn *rod.Element) bool {
	res, err := btn.Eval(`() => {
		const r = this.getBoundingClientRect();
		if (r.width < 2 && r.height < 2) return false;
		if (this.disabled || this.getAttribute('aria-disabled') === 'true') return false;
		const s = getComputedStyle(this);
		if (s.display === 'none' || s.visibility === 'hidden' || s.pointerEvents === 'none') return false;
		return true;
	}`)
	return err == nil && res.Value.Bool()
}

// jsClickUploadByText locates a radio/button whose visible text matches a
// regex like /upload (a |an |new )?(resum|cover|cv)/i and clicks it. Returns
// true on success. Used as a Seek-only fallback when the named radio probe
// (jsFillRadio) misses because Seek renamed the radio group.
const jsClickUploadByText = `(pattern) => {
	const re = new RegExp(pattern, 'i');
	function isVisible(el) {
		try {
			const r = el.getBoundingClientRect();
			if (r.width === 0 && r.height === 0) return false;
			let n = el;
			while (n && n !== document.documentElement) {
				const s = getComputedStyle(n);
				if (s.display === 'none' || s.visibility === 'hidden') return false;
				n = n.parentElement;
			}
			return true;
		} catch (e) { return false; }
	}
	const labels = [...document.querySelectorAll('label, [role="radio"], button, a, span')]
		.filter(isVisible)
		.filter(el => re.test((el.textContent || '').trim()));
	for (const lbl of labels) {
		// Prefer clicking an associated radio input when present.
		const forId = lbl.getAttribute && lbl.getAttribute('for');
		if (forId) {
			const r = document.getElementById(forId);
			if (r) { r.click(); return true; }
		}
		const inner = lbl.querySelector && lbl.querySelector('input[type="radio"]');
		if (inner) { inner.click(); return true; }
		try { lbl.click(); return true; } catch (e) {}
	}
	return false;
}`

// seekDumpUploadDebug writes a snapshot of the current page so we can update
// upload selectors when Seek changes its DOM. Best-effort; failures are logged
// but do not propagate. Each call is bounded by hard timeouts so a partially
// dead CDP socket can't freeze the whole bot (we've seen multi-hour stalls
// when page.HTML()/Screenshot() hung on a closing socket).
func seekDumpUploadDebug(page *rod.Page, slug string) {
	htmlPath := "debug_seek_upload_" + slug + ".html"
	pngPath := "debug_seek_upload_" + slug + ".png"
	tp := page.Timeout(5 * time.Second)
	if html, err := tp.HTML(); err == nil {
		if werr := os.WriteFile(htmlPath, []byte(html), 0o644); werr == nil {
			log.Warn().Msgf("seek: upload debug HTML saved to %s", htmlPath)
		}
	} else {
		log.Warn().Err(err).Msgf("seek: upload debug HTML capture timed out for %s", slug)
	}
	if shot, err := tp.Screenshot(false, nil); err == nil {
		if werr := os.WriteFile(pngPath, shot, 0o644); werr == nil {
			log.Warn().Msgf("seek: upload debug screenshot saved to %s", pngPath)
		}
	} else {
		log.Warn().Err(err).Msgf("seek: upload debug screenshot capture timed out for %s", slug)
	}
}

// seekUploadResume activates the "Upload a resumé" radio and sets the resume file.
// If Seek's 10-resume library limit dialog appears, it selects the oldest bot-uploaded
// resume from the dropdown, clicks the delete button (no further confirmation), then retries.
func (b *Bot) seekUploadResume(page *rod.Page, filePath string) error {
	radioSelected := false
	if ok, _ := page.Eval(jsFillRadio, "resume-method", "upload"); ok != nil && ok.Value.Bool() {
		radioSelected = true
	}
	if !radioSelected {
		// Text-based fallback for renamed radio groups.
		if ok, _ := page.Eval(jsClickUploadByText, `^upload( a| an| new)? (resum|cv)`); ok != nil && ok.Value.Bool() {
			radioSelected = true
			log.Info().Msg("seek: resume upload radio selected via text fallback")
		} else {
			log.Warn().Msg("seek: resume-method=upload radio not found, upload may fail")
		}
	}
	time.Sleep(600 * time.Millisecond)

	// Handle "Resumé limit reached" dialog if it appeared after selecting upload.
	if err := seekHandleResumeLimitDialog(page); err != nil {
		return fmt.Errorf("seek: resume limit dialog: %w", err)
	}

	// Primary selector first; fall back to any visible PDF-accepting file input
	// inside the apply form anchor when Seek renames the testid.
	inputs, err := page.Elements("[data-testid='resumeFileInput'] input[type='file']")
	if err != nil || len(inputs) == 0 {
		fallback, ferr := page.Elements("[data-testid='quick-apply-form'] input[type='file'][accept*='pdf'], [data-automation='apply-form'] input[type='file'][accept*='pdf']")
		if ferr == nil && len(fallback) > 0 {
			log.Info().Msg("seek: resume file input located via fallback selector")
			inputs = fallback
		} else {
			seekDumpUploadDebug(page, "resume")
			return fmt.Errorf("resume file input not found after selecting upload radio")
		}
	}
	return inputs[0].SetFiles([]string{filePath})
}

// seekHandleResumeLimitDialog checks for Seek's 10-resume limit dialog. If present,
// it selects the oldest bot-uploaded resume from the dropdown and clicks the delete
// button. Seek closes the dialog and deletes in the background — no further
// confirmation dialog appears.
func seekHandleResumeLimitDialog(page *rod.Page) error {
	els, err := page.Elements("[data-testid='10-resume-limit-text'], #docLimitExceededDropdown")
	if err != nil || len(els) == 0 {
		return nil // no dialog
	}
	log.Info().Msg("seek: resume library full, selecting oldest bot-uploaded resume for deletion")

	// Select the oldest bot-uploaded resume from the dropdown.
	// Bot uploads are named like "D/M/YY - resume.pdf". Walk from last (oldest)
	// to first (newest); fall back to the very last option if no pattern matches.
	res, evalErr := page.Eval(`() => {
		const sel = document.getElementById('docLimitExceededDropdown');
		if (!sel) return null;
		const opts = [...sel.options];
		for (let i = opts.length - 1; i >= 0; i--) {
			const text = opts[i].text.toLowerCase();
			if (text.includes('resume.pdf')) {
				sel.value = opts[i].value;
				sel.dispatchEvent(new Event('change', { bubbles: true }));
				return opts[i].text;
			}
		}
		if (opts.length > 0) {
			const last = opts[opts.length - 1];
			sel.value = last.value;
			sel.dispatchEvent(new Event('change', { bubbles: true }));
			return last.text;
		}
		return null;
	}`)
	if evalErr != nil {
		return fmt.Errorf("evaluate dropdown: %w", evalErr)
	}
	if res.Value.Str() != "" {
		log.Info().Str("deleted_resume", res.Value.Str()).Msg("seek: selected resume for deletion")
	}

	time.Sleep(300 * time.Millisecond)

	// Click the delete button — dialog closes and deletion proceeds in background.
	deleteBtn, btnErr := page.Timeout(8 * time.Second).Element(`[data-automation="10-resume-delete"]`)
	if btnErr != nil {
		return fmt.Errorf("delete button not found: %w", btnErr)
	}
	if clickErr := deleteBtn.Click(proto.InputMouseButtonLeft, 1); clickErr != nil {
		return fmt.Errorf("click delete button: %w", clickErr)
	}
	log.Info().Msg("seek: resume limit dialog dismissed, waiting for deletion")

	// Wait for dialog to close and deletion to complete before retrying upload.
	time.Sleep(1500 * time.Millisecond)
	return nil
}

// seekUploadCoverLetter activates the "Upload a cover letter" radio and sets the cover letter file.
func (b *Bot) seekUploadCoverLetter(page *rod.Page, filePath string) error {
	radioSelected := false
	if ok, _ := page.Eval(jsFillRadio, "coverLetter-method", "upload"); ok != nil && ok.Value.Bool() {
		radioSelected = true
	}
	if !radioSelected {
		if ok, _ := page.Eval(jsClickUploadByText, `^upload( a| an| new)? cover`); ok != nil && ok.Value.Bool() {
			radioSelected = true
			log.Info().Msg("seek: cover letter upload radio selected via text fallback")
		} else {
			log.Warn().Msg("seek: coverLetter-method=upload radio not found, upload may fail")
		}
	}
	time.Sleep(600 * time.Millisecond)
	if inputs, err := page.Elements("[data-testid='coverLetterFileInput'] input[type='file']"); err == nil && len(inputs) > 0 {
		return inputs[0].SetFiles([]string{filePath})
	}
	if fallback, ferr := page.Elements("[data-testid='quick-apply-form'] input[type='file'][accept*='pdf'], [data-automation='apply-form'] input[type='file'][accept*='pdf']"); ferr == nil && len(fallback) > 1 {
		// Skip the first match — likely the resume input — and use the next one.
		log.Info().Msg("seek: cover letter file input located via fallback selector")
		return fallback[1].SetFiles([]string{filePath})
	}
	seekDumpUploadDebug(page, "cover_letter")
	return fmt.Errorf("cover letter file input not found after selecting upload radio")
}

// ── DB helpers (Seek-specific wrappers) ───────────────────────────────────

func (b *Bot) recordSeekApplied(job seekJob, resumePath, coverPath string, score int, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := appdb.ExecWithRetry(b.cfg.DB,
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("seek: failed to record applied job")
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenApplied)
	}
}

func (b *Bot) recordSeekSkipped(job seekJob, reason string, score int, reasoning string, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := appdb.ExecWithRetry(b.cfg.DB,
		`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("seek: failed to record skipped job")
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenSkipped)
	}
}

func (b *Bot) isSeekJobBlacklisted(job seekJob) bool {
	if domain.LocationMatchesBlacklist(job.Location, b.cfg.Preferences.LocationBlacklist) {
		return true
	}
	for _, c := range b.cfg.Preferences.CompanyBlacklist {
		if strings.EqualFold(job.Company, c) || strings.Contains(strings.ToLower(job.Company), strings.ToLower(c)) {
			return true
		}
	}
	for _, t := range b.cfg.Preferences.TitleBlacklist {
		if strings.Contains(strings.ToLower(job.Title), strings.ToLower(t)) {
			return true
		}
	}
	return false
}
