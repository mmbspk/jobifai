package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/scraper"
)

func init() {
	registerRunner(domain.PlatformSeek, platformRunnerFunc(runSeek))
}

const seekQuickApply = "quick apply"

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

// ── Session helpers ───────────────────────────────────────────────────────

// isSeekLoginPage returns true when the URL indicates Seek has redirected the
// browser to a login or OAuth page — meaning the saved session is expired.
func isSeekLoginPage(u string) bool {
	return strings.Contains(u, "/login") ||
		strings.Contains(u, "/oauth") ||
		strings.Contains(u, "sign-in")
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

	// Verify session is still valid after browser launch.
	if info, e := page.Info(); e == nil && isSeekLoginPage(info.URL) {
		log.Error().Msg("seek: session expired, re-login via Settings → Secrets")
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

	// Resolve location via Seek's autocomplete once before keyword loop.
	resolvedLocation := ""
	if len(b.cfg.Preferences.Locations) > 0 {
		resolvedLocation = b.resolveSeekLocation(page, b.cfg.Preferences.Locations[0])
	}

	for _, keyword := range b.cfg.Preferences.Positions {
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Str("keyword", keyword).Msgf("seek: stopped, %s", reason)
			return
		}
		if appliedToday >= limit {
			log.Info().Int("limit", limit).Msg("seek: stopped, daily application limit reached")
			return
		}
		b.SetKeyword(keyword)
		n := b.processSeekKeyword(ctx, br, page, keyword, limit-appliedToday, resolvedLocation)
		appliedToday += n

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
			appliedToday += b.processSeekKeyword(ctx, br, page, keyword, limit-appliedToday, resolvedLocation)
		}
	}
	b.SetKeyword("")
	log.Info().Int("applied_today", appliedToday).Msg("seek: stopped, all keywords processed, no more jobs found")
}

// ── Keyword loop ──────────────────────────────────────────────────────────

func (b *Bot) processSeekKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int, resolvedLocation string) int {
	jobs, err := b.scrapeSeekJobs(ctx, page, keyword, resolvedLocation)
	if err != nil {
		log.Error().Err(err).Str("keyword", keyword).Msg("seek: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("seek: no new jobs found for keyword")
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
	}
	log.Info().Str("keyword", keyword).Int("applied", applied).Msg("seek: keyword done")
	return applied
}

// ── Job scraping ──────────────────────────────────────────────────────────

func (b *Bot) scrapeSeekJobs(ctx context.Context, page *rod.Page, keyword, resolvedLocation string) ([]seekJob, error) {
	searchURL := buildSeekSearchURL(keyword, b.cfg.Preferences, resolvedLocation)
	log.Info().Str("keyword", keyword).Msg("seek: searching")
	log.Debug().Str("url", searchURL).Msg("seek: navigating to search URL")

	if err := page.Navigate(searchURL); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}
	_ = page.WaitStable(500 * time.Millisecond)
	log.Info().Str("keyword", keyword).Msg("seek: page loaded, scraping cards")

	cap := b.cfg.Settings.MaxJobsPerKeyword
	if cap <= 0 {
		cap = 25
	}

	seen := map[string]bool{}
	var jobs []seekJob

	for attempt := 0; len(jobs) < cap; attempt++ {
		cards, err := page.Elements("article[data-job-id]")
		if err != nil || len(cards) == 0 {
			cards, _ = page.Elements("[data-testid='job-card']")
		}
		if len(cards) == 0 && attempt == 0 {
			// Check if Seek is showing a genuine zero-results page.
			if _, zrErr := page.Element("[data-automation='search-zero-results']"); zrErr == nil {
				log.Info().Str("keyword", keyword).Msg("seek: search returned no results for keyword")
				return nil, nil
			}
			// Unexpected: page loaded but no cards and no zero-results indicator — selector may need updating.
			log.Warn().Str("keyword", keyword).Msg("seek: no job cards found, selectors may need updating")
			if html, err := page.HTML(); err == nil {
				_ = os.WriteFile("debug_seek_page.html", []byte(html), 0o644)
				log.Warn().Msg("seek: page HTML saved to debug_seek_page.html")
			}
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_seek_page.png", shot, 0o644)
				log.Warn().Msg("seek: screenshot saved to debug_seek_page.png")
			}
			return nil, nil
		}

		prevCount := len(jobs)
		for _, card := range cards {
			job := extractSeekJob(card)
			if job.ID != "" && !seen[job.ID] {
				seen[job.ID] = true
				jobs = append(jobs, job)
			}
		}
		log.Info().Msgf("seek: scroll %d, found %d cards (%d new)", attempt+1, len(cards), len(jobs)-prevCount)

		// Stop if we hit the cap or no new cards appeared after scrolling.
		if len(jobs) >= cap || (attempt > 0 && len(jobs) == prevCount) {
			break
		}

		// Scroll to the last card to trigger Seek's infinite scroll observer.
		if len(cards) > 0 {
			_ = cards[len(cards)-1].ScrollIntoView()
		} else {
			_, _ = page.Eval(`() => window.scrollTo(0, document.body.scrollHeight)`)
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
	// Best-effort Quick Apply detection on listing card.
	if _, err := el.Element("[data-automation='quick-apply-label']"); err == nil {
		job.EasyApply = true
	} else if txt, err := el.Text(); err == nil && strings.Contains(strings.ToLower(txt), seekQuickApply) {
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

// seekLocationSlug maps free-text location entries to Seek's path-based location slugs
// used in URLs like https://au.seek.com/jobs/in-{slug}?keywords=...
var seekLocationSlug = map[string]string{
	"victoria":            "Victoria VIC",
	"vic":                 "Victoria VIC",
	"victoria, australia": "Victoria VIC",
	"melbourne":           "Melbourne-VIC",
	"new south wales":     "New-South-Wales",
	"nsw":                 "New-South-Wales",
	"sydney":              "Sydney-NSW",
	"queensland":          "Queensland",
	"qld":                 "Queensland",
	"brisbane":            "Brisbane-QLD",
	"western australia":   "Western-Australia",
	"wa":                  "Western-Australia",
	"perth":               "Perth-WA",
	"south australia":     "South-Australia",
	"sa":                  "South-Australia",
	"adelaide":            "Adelaide-SA",
	"tasmania":            "Tasmania",
	"tas":                 "Tasmania",
	"hobart":              "Hobart-TAS",
	"northern territory":  "Northern-Territory",
	"nt":                  "Northern-Territory",
	"darwin":              "Darwin-NT",
	"act":                 "Australian-Capital-Territory",
	"canberra":            "Canberra-ACT",
}

func seekLocationPath(loc string) string {
	if slug, ok := seekLocationSlug[strings.ToLower(strings.TrimSpace(loc))]; ok {
		return "/jobs/in-" + url.PathEscape(slug)
	}
	if loc != "" && strings.ToLower(loc) != "all australia" {
		return "/jobs/in-" + url.PathEscape(strings.TrimSpace(loc))
	}
	return "/jobs"
}

// resolveSeekLocation uses the browser to type the user's location into Seek's
// autocomplete field and returns the first suggestion — the exact value Seek
// accepts in the ?where= query parameter. Falls back to the raw input on error.
func (b *Bot) resolveSeekLocation(page *rod.Page, locInput string) string {
	if err := page.Navigate("https://au.seek.com/jobs"); err != nil {
		return locInput
	}
	_ = page.WaitLoad()
	_ = page.WaitStable(1 * time.Second)

	whereEl, err := page.Element("#SearchBar__Where")
	if err != nil {
		log.Warn().Msg("seek: location input not found on page")
		return locInput
	}

	// Click to focus the field.
	if err := whereEl.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return locInput
	}
	time.Sleep(300 * time.Millisecond)

	// Clear any existing value.
	_ = whereEl.SelectAllText()
	time.Sleep(100 * time.Millisecond)

	// Type one character at a time — Seek's autocomplete only fires on real keystroke events.
	for _, r := range locInput {
		_ = page.InsertText(string(r))
		time.Sleep(60 * time.Millisecond)
	}

	// Wait for the first autocomplete suggestion to appear.
	opt, err := page.Timeout(8 * time.Second).Element("#SearchBar__Where-menu [role='option']")
	if err != nil {
		// Fallback: generic listbox option
		opt, err = page.Timeout(3 * time.Second).Element("[role='listbox'] [role='option']")
		if err != nil {
			log.Warn().Str("input", locInput).Msg("seek: location autocomplete did not appear, using path-based fallback")
			return ""
		}
	}
	text, err := opt.Text()
	if err != nil || text == "" {
		return ""
	}
	log.Info().Msgf("seek: resolved location %q → %q", locInput, text)
	return text
}

func buildSeekSearchURL(keyword string, prefs domain.WorkPreferences, resolvedLocation string) string {
	params := url.Values{}
	params.Set("keywords", keyword)

	locationPath := "/jobs"
	if resolvedLocation != "" {
		params.Set("where", resolvedLocation)
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

	// Work arrangement (Seek codes: 1=onsite, 2=hybrid, 3=remote)
	var arrangements []string
	if prefs.Onsite {
		arrangements = append(arrangements, "1")
	}
	if prefs.Hybrid {
		arrangements = append(arrangements, "2")
	}
	if prefs.Remote {
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

	// Docs generated lazily at the file-upload step, only when toggle is on.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: job.Company, Title: job.Title}, jobDesc: details.Description}

	if b.cfg.RequireReview {
		b.saveSeekPendingReview(&domain.PendingReview{
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
		// Not a Quick Apply job, queue for manual application via Top Matches.
		b.saveSeekPendingReview(&domain.PendingReview{
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
		return false
	}

	return b.submitSeekApplication(ctx, br, job, lazy, score, halalVerdict, llmBefore)
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
			log.Warn().Msgf("seek: detail page redirected to login for %q — session may be expired", job.Title)
		}
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

	// Authoritative Quick Apply detection: Seek-hosted form = Quick Apply; external href = manual apply.
	// Both strategies use Elements() (immediate querySelectorAll) — no polling, no shared context expiry.
	if !job.EasyApply {
		// Strategy 1: data-automation attribute — present on all standard Seek apply buttons.
		if elems, err := page.Elements("[data-automation='job-detail-apply'], [data-automation='job-detail-apply-link'], button[data-automation*='apply']"); err == nil && len(elems) > 0 {
			btn := elems[0]
			if txt, err := btn.Text(); err == nil {
				if strings.Contains(strings.ToLower(strings.TrimSpace(txt)), seekQuickApply) {
					job.EasyApply = true
				}
			}
			if !job.EasyApply {
				if href, _ := btn.Attribute("href"); href == nil {
					job.EasyApply = true // no href = Seek-hosted modal
				} else {
					h := *href
					// Relative or seek.com href = Seek-hosted.
					if h == "" || strings.HasPrefix(h, "/") || strings.Contains(h, "seek.com") {
						job.EasyApply = true
					}
					// External http(s) link to another domain = manual apply only.
				}
			}
		}

		// Strategy 2: text-scan fallback — catches UI changes where data-automation differs.
		if !job.EasyApply {
			if btns, err := page.Elements("button, a[href]"); err == nil {
				for _, el := range btns {
					if txt, err := el.Text(); err == nil && strings.Contains(strings.ToLower(txt), seekQuickApply) {
						job.EasyApply = true
						break
					}
				}
			}
		}

		if job.EasyApply {
			log.Info().Msgf("seek: Quick Apply detected: %q @ %s", job.Title, job.Company)
		} else {
			log.Info().Msgf("seek: no Quick Apply on detail page, routing to Top Matches: %q @ %s", job.Title, job.Company)
		}
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

func (b *Bot) saveSeekPendingReview(p *domain.PendingReview) {
	b.savePendingReview(p)
	if p.EasyApply {
		log.Info().Msgf("seek: queued for review, %q @ %s", p.Role, p.Company)
	} else {
		log.Info().Msgf("seek: added to Top Matches (manual apply), %q @ %s", p.Role, p.Company)
	}
}

// ── Application submission ─────────────────────────────────────────────────

func (b *Bot) submitSeekApplication(ctx context.Context, br *rod.Browser, job seekJob, lazy *lazyDocGen, score int, halalVerdict []byte, llmBefore llm.UsageSnapshot) bool {
	jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		log.Error().Err(err).Msg("seek: open job page")
		return false
	}
	defer jobPage.Close()

	if err := b.seekApply(ctx, jobPage, lazy); err != nil {
		log.Error().Err(err).Str("job", job.Title).Msg("seek: apply failed")
		b.recordSeekSkipped(job, "seek apply: "+err.Error(), score, "", nil)
		return false
	}
	resume, cover := lazy.get()
	b.recordSeekApplied(job, resume, cover, score, halalVerdict)
	b.logApplied(job.Title, job.Company, domain.PlatformSeek, llmBefore)
	return true
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

	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}
	// Give auth0-spa-js time to complete its silent re-auth via hidden iframe.
	_ = page.WaitStable(3 * time.Second)

	// Persist the freshly-rotated refresh token so the NEXT browser launch
	// starts with a valid token (auth0 rotation means the token used to land
	// here is already consumed and must not be presented again).
	b.seekPersistSession(page)

	// Verify the session is genuinely authenticated before clicking Quick Apply.
	// Seek's SPA loads without redirecting even when auth0 silent re-auth fails
	// (it handles login_required gracefully), so a URL-only check is insufficient.
	if loggedIn, err := b.seekIsLoggedIn(page); err == nil && !loggedIn {
		return fmt.Errorf("seek session expired — not logged in on job page, re-add your Seek session in Settings → Secrets")
	}

	if info, err := page.Info(); err == nil {
		if isSeekLoginPage(info.URL) {
			return fmt.Errorf("seek session expired, re-login via Settings → Secrets")
		}
	}

	if err := b.seekClickQuickApply(page); err != nil {
		return err
	}
	log.Info().Msg("seek: Quick Apply button clicked, waiting for form")
	// Start doc generation immediately — runs concurrently while the form loads.
	lazy.preload()
	b.humanPause()

	if onLogin, err := b.seekWaitPastLogin(page); err != nil {
		return fmt.Errorf("wait form load: %w", err)
	} else if onLogin {
		// Auth0 could not silently redirect back — session is genuinely expired.
		return fmt.Errorf("seek session expired — Quick Apply redirected to login, re-add your Seek session in Settings → Secrets")
	}

	// Generate docs (lazy — cached, only if the toggle is on).
	resumePath, coverPath := lazy.get()

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
		// Fill any screening questions visible on the current page.
		b.fillFormStep(ctx, page, lazy)
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
		if err := actionBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click %q step %d: %w", btnText, step+1, err)
		}

		if isSubmit {
			log.Info().Msgf("seek: submit clicked (%q), verifying confirmation", btnText)
			b.humanPause()
			_ = page.WaitLoad()
			_ = page.WaitStable(2 * time.Second)

			// 1. Seek data-automation attributes for success/confirmation.
			if _, verr := page.Timeout(8 * time.Second).Element(
				"[data-automation='application-success'], [data-automation='confirmation-page'], " +
					"[data-automation='application-submitted'], [data-automation='quick-apply-success'], " +
					"[data-automation='job-applied-message']",
			); verr == nil {
				log.Info().Msg("seek: Quick Apply submitted successfully ✓")
				return nil
			}

			// 2. URL contains success/confirmation keywords.
			if info, _ := page.Info(); strings.Contains(info.URL, "confirm") || strings.Contains(info.URL, "success") || strings.Contains(info.URL, "thank") || strings.Contains(info.URL, "applied") {
				log.Info().Msg("seek: Quick Apply submitted successfully ✓")
				return nil
			}

			// 3. Any heading or paragraph text indicates success.
			if elems, err := page.Elements("[data-automation='application-success-title'], h1, h2, h3, p"); err == nil {
				for _, el := range elems {
					if txt, err := el.Text(); err == nil {
						lower := strings.ToLower(txt)
						if strings.Contains(lower, "submitted") || strings.Contains(lower, "applied") ||
							strings.Contains(lower, "success") || strings.Contains(lower, "application sent") ||
							strings.Contains(lower, "good luck") || strings.Contains(lower, "fingers crossed") {
							log.Info().Msgf("seek: Quick Apply submitted successfully ✓ (text: %q)", txt)
							return nil
						}
					}
				}
			}

			// 4. The Quick Apply form panel is gone — form closed after submit.
			if hasForm, _ := page.Eval(`() => !!document.querySelector('[data-automation="apply-form"], [data-testid="quick-apply-form"]')`); hasForm != nil && !hasForm.Value.Bool() {
				log.Info().Msg("seek: Quick Apply submitted successfully ✓ (form closed)")
				return nil
			}

			// 5. Dump page title + URL for diagnosis before giving up.
			if info, _ := page.Info(); info != nil {
				log.Warn().Str("url", info.URL).Str("title", info.Title).Msg("seek: could not confirm submission — page state")
			}
			return fmt.Errorf("could not confirm submission")
		}

		log.Info().Msgf("seek: form step %d, clicked %q", step+1, btnText)
		b.humanPause()
		_ = page.WaitLoad()
		_ = page.WaitStable(500 * time.Millisecond)
	}

	return fmt.Errorf("could not complete Quick Apply: exceeded maximum form steps")
}

// seekWaitPastLogin waits for the page to load after a Quick Apply click.
// If the browser lands on a login page, it polls for up to 15 s to see if
// auth0 silently redirects back (it does when the session is still valid).
// Returns (true, nil) if still on a login page after the timeout — meaning
// the session is genuinely expired. Returns (false, nil) on success.
func (b *Bot) seekWaitPastLogin(page *rod.Page) (onLogin bool, err error) {
	if err := page.WaitLoad(); err != nil {
		return false, err
	}
	_ = page.WaitStable(2 * time.Second)

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
			_ = page.WaitLoad()
			_ = page.WaitStable(2 * time.Second)
			return false, nil
		}
	}
	return true, nil // session expired — no silent redirect happened
}

// seekIsLoggedIn checks whether Seek's SPA considers the user authenticated.
// Seek loads the page even when auth0 silent re-auth fails, so a URL check alone
// is insufficient — we must probe the DOM for logged-in state indicators.
func (b *Bot) seekIsLoggedIn(page *rod.Page) (bool, error) {
	res, err := page.Eval(`() => {
		if (document.querySelector('[data-automation="account-nav"], [data-automation="signed-in-nav"], [aria-label="My account"]')) return 'yes';
		if (document.querySelector('[data-automation="sign-in"], a[href*="/oauth/login"]')) return 'no';
		if (document.querySelector('a[href="/dashboard"], [data-automation="profile-link"]')) return 'yes';
		return 'unknown';
	}`)
	if err != nil {
		return true, nil // assume logged in on eval error
	}
	switch res.Value.Str() {
	case "no":
		return false, nil
	default:
		return true, nil // "yes" or "unknown" — optimistically proceed
	}
}

// seekPersistSession snapshots all browser cookies (including login.seek.com)
// and saves them back to the session store. Called after each auth0 silent
// re-auth so the next browser launch uses the freshest refresh token.
func (b *Bot) seekPersistSession(page *rod.Page) {
	if b.cfg.Sessions == nil {
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
	if err := b.cfg.Sessions.Save(b.cfg.UserID, string(b.cfg.Platform), "session", fresh); err != nil {
		log.Warn().Err(err).Msg("seek: failed to refresh session cookies")
	} else {
		log.Debug().Msg("seek: session cookies refreshed after job detail page auth0 re-auth")
	}
}

// seekClickQuickApply finds the Quick Apply button, checks it's not external,
// and clicks it. Extracted so seekApply can call it twice (initial + post-login retry).
func (b *Bot) seekClickQuickApply(page *rod.Page) error {
	applyBtn, err := page.Element("[data-automation='job-detail-apply']")
	if err != nil {
		applyBtn, err = page.Element("a[href*='/apply'], button[data-automation*='apply']")
		if err != nil {
			return fmt.Errorf("apply button not found: %w", err)
		}
	}
	if href, e := applyBtn.Attribute("href"); e == nil && href != nil {
		h := *href
		if h != "" && !strings.Contains(h, "seek.com") && strings.HasPrefix(h, "http") {
			return fmt.Errorf("external application")
		}
	}
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
func (b *Bot) seekFindActionButton(page *rod.Page) (*rod.Element, bool) {
	// Check explicit submit selectors first.
	for _, sel := range []string{
		"button[data-testid='review-submit-application']",
		"[data-automation='review-submit-button']",
		"button[data-testid='review-submit-button']",
		"button[data-testid='submit-application']",
	} {
		if elems, err := page.Elements(sel); err == nil && len(elems) > 0 {
			return elems[0], true
		}
	}

	// Continue/Next button (intermediate steps).
	if elems, err := page.Elements("button[data-testid='continue-button']"); err == nil && len(elems) > 0 {
		return elems[0], false
	}

	// Text-based fallback: scan all visible buttons for submit or continue keywords.
	if btns, err := page.Elements("button"); err == nil {
		for _, btn := range btns {
			txt, err := btn.Text()
			if err != nil {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(txt))
			if strings.Contains(lower, "submit") {
				return btn, true
			}
		}
		for _, btn := range btns {
			txt, err := btn.Text()
			if err != nil {
				continue
			}
			lower := strings.ToLower(strings.TrimSpace(txt))
			if strings.Contains(lower, "continue") || strings.Contains(lower, "next") {
				return btn, false
			}
		}
	}
	return nil, false
}

// seekUploadResume activates the "Upload a resumé" radio and sets the resume file.
// If Seek's 10-resume library limit dialog appears, it selects the oldest bot-uploaded
// resume from the dropdown, clicks the delete button (no further confirmation), then retries.
func (b *Bot) seekUploadResume(page *rod.Page, filePath string) error {
	if ok, _ := page.Eval(jsFillRadio, "resume-method", "upload"); ok != nil && !ok.Value.Bool() {
		log.Warn().Msg("seek: resume-method=upload radio not found, upload may fail")
	}
	time.Sleep(600 * time.Millisecond)

	// Handle "Resumé limit reached" dialog if it appeared after selecting upload.
	if err := seekHandleResumeLimitDialog(page); err != nil {
		return fmt.Errorf("seek: resume limit dialog: %w", err)
	}

	inputs, err := page.Elements("[data-testid='resumeFileInput'] input[type='file']")
	if err != nil || len(inputs) == 0 {
		return fmt.Errorf("resume file input not found after selecting upload radio")
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
	deleteBtn, btnErr := page.Element(`[data-automation="10-resume-delete"]`)
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
	if ok, _ := page.Eval(jsFillRadio, "coverLetter-method", "upload"); ok != nil && !ok.Value.Bool() {
		log.Warn().Msg("seek: coverLetter-method=upload radio not found, upload may fail")
	}
	time.Sleep(600 * time.Millisecond)
	if inputs, err := page.Elements("[data-testid='coverLetterFileInput'] input[type='file']"); err == nil && len(inputs) > 0 {
		return inputs[0].SetFiles([]string{filePath})
	}
	return fmt.Errorf("cover letter file input not found after selecting upload radio")
}

// ── DB helpers (Seek-specific wrappers) ───────────────────────────────────

func (b *Bot) recordSeekApplied(job seekJob, resumePath, coverPath string, score int, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("seek: failed to record applied job")
	}
}

func (b *Bot) recordSeekSkipped(job seekJob, reason string, score int, reasoning string, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("seek: failed to record skipped job")
	}
}

func (b *Bot) isSeekJobBlacklisted(job seekJob) bool {
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
