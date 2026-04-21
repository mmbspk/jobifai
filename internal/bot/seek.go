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
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/scraper"
)

func init() {
	registerRunner(domain.PlatformSeek, platformRunnerFunc(runSeek))
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

	limit := b.cfg.Settings.HumanBehavior.DailyApplicationLimit
	if limit == 0 {
		limit = 40
	}
	appliedToday := b.countAppliedToday()
	log.Info().Int("applied_today", appliedToday).Int("limit", limit).Msg("seek: starting — daily progress")

	// Process previously approved jobs first.
	if appliedToday < limit {
		appliedToday += b.processApprovedQueue(ctx, br, limit-appliedToday)
	}

	for _, keyword := range b.cfg.Preferences.Positions {
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Str("keyword", keyword).Msgf("seek: stopped — %s", reason)
			return
		}
		if appliedToday >= limit {
			log.Info().Int("limit", limit).Msg("seek: stopped — daily application limit reached")
			return
		}
		b.SetKeyword(keyword)
		appliedToday += b.processSeekKeyword(ctx, br, page, keyword, limit-appliedToday)
	}
	b.SetKeyword("")
	log.Info().Int("applied_today", appliedToday).Msg("seek: stopped — all keywords processed, no more jobs found")
}

// ── Keyword loop ──────────────────────────────────────────────────────────

func (b *Bot) processSeekKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int) int {
	jobs, err := b.scrapeSeekJobs(ctx, page, keyword)
	if err != nil {
		log.Error().Err(err).Str("keyword", keyword).Msg("seek: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("seek: no new jobs found for keyword")
		return 0
	}
	log.Info().Msgf("seek: found %d jobs for %q — processing", len(jobs), keyword)
	applied := 0
	for _, job := range jobs {
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Str("keyword", keyword).Msgf("seek: stopped mid-keyword — %s", reason)
			return applied
		}
		if applied >= remaining {
			log.Info().Str("keyword", keyword).Int("remaining", remaining).Msg("seek: stopped mid-keyword — daily limit reached")
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

func (b *Bot) scrapeSeekJobs(ctx context.Context, page *rod.Page, keyword string) ([]seekJob, error) {
	searchURL := buildSeekSearchURL(keyword, b.cfg.Preferences)
	log.Info().Str("url", searchURL).Msg("seek: searching")

	if err := page.Navigate(searchURL); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}
	_ = page.WaitStable(500 * time.Millisecond)

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
			log.Warn().Str("keyword", keyword).Msg("seek: no job cards found — selectors may need updating")
			// Dump page HTML and a screenshot for selector debugging.
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

	// Title — primary: data-automation, fallback: h3 > a
	if t, err := el.Element("[data-automation='job-list-item-title']"); err == nil {
		job.Title, _ = t.Text()
	} else if t, err := el.Element("h3 a, h2 a"); err == nil {
		job.Title, _ = t.Text()
	}

	// Company — try several Seek automation attributes and class-based fallbacks.
	if c, err := el.Element("[data-automation='job-list-item-company-name']"); err == nil {
		job.Company, _ = c.Text()
	} else if c, err := el.Element("[data-automation='advertiser-name']"); err == nil {
		job.Company, _ = c.Text()
	} else if c, err := el.Element("[data-automation*='company'], [data-automation*='advertiser']"); err == nil {
		job.Company, _ = c.Text()
	} else if c, err := el.Element("[class*='company'], [class*='Company'], [class*='advertiser'], [class*='Advertiser']"); err == nil {
		job.Company, _ = c.Text()
	}
	if job.Company == "" {
		// Log outer HTML to diagnose selector mismatches.
		if h, err := el.HTML(); err == nil {
			log.Debug().Str("id", job.ID).Str("card_html", h[:min(len(h), 800)]).Msg("seek: company empty — card html sample")
		}
	}

	// Location — primary: data-automation, fallback: class contains "location"
	if l, err := el.Element("[data-automation='job-card-location']"); err == nil {
		job.Location, _ = l.Text()
	} else if l, err := el.Element("[class*='location'], [class*='Location']"); err == nil {
		job.Location, _ = l.Text()
	}

	// PostedDate — extracted from the listing card time element.
	if t, err := el.Element("time[datetime]"); err == nil {
		if dt, e := t.Attribute("datetime"); e == nil && dt != nil {
			job.PostedDate = *dt
		} else {
			job.PostedDate, _ = t.Text()
		}
	} else if t, err := el.Element("[data-automation*='date'], [data-automation*='Date']"); err == nil {
		job.PostedDate, _ = t.Text()
	}

	// Applied indicator — Seek shows a badge on cards for jobs already applied to.
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
	} else if txt, err := el.Text(); err == nil && strings.Contains(strings.ToLower(txt), "quick apply") {
		job.EasyApply = true
	}

	// URL — prefer canonical job URL built from ID; also try extracting from link href.
	if job.ID != "" {
		job.URL = "https://www.seek.com.au/job/" + job.ID
	} else if a, err := el.Element("a[href*='/job/']"); err == nil {
		if href, e := a.Attribute("href"); e == nil && href != nil {
			h := *href
			if strings.HasPrefix(h, "/") {
				h = "https://www.seek.com.au" + h
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

func buildSeekSearchURL(keyword string, prefs domain.WorkPreferences) string {
	params := url.Values{}
	params.Set("keywords", keyword)

	// Location
	if len(prefs.Locations) > 0 {
		params.Set("where", prefs.Locations[0])
	} else {
		params.Set("where", "All Australia")
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

	return "https://www.seek.com.au/jobs?" + params.Encode()
}

// ── Per-job pipeline ──────────────────────────────────────────────────────

func (b *Bot) processSeekJob(ctx context.Context, br *rod.Browser, job seekJob) bool {
	log.Info().Msgf("seek: processing — %q @ %s", job.Title, job.Company)

	if b.alreadyApplied(job.ID) {
		log.Info().Msgf("seek: skip — already applied to %q @ %s", job.Title, job.Company)
		return false
	}
	// Card-level applied indicator.
	if job.AlreadyApplied {
		b.recordSeekApplied(job, "", "", 0, nil)
		log.Info().Msgf("seek: skip — already applied badge on card: %q @ %s", job.Title, job.Company)
		return false
	}
	if b.isSeekJobBlacklisted(job) {
		b.recordSeekSkipped(job, "blacklisted", 0, "", nil)
		log.Info().Msgf("seek: skip — blacklisted: %q @ %s", job.Title, job.Company)
		return false
	}

	details := b.fetchSeekJobDetails(ctx, br, &job)
	// Detail page may have updated AlreadyApplied (covers manual applications).
	if job.AlreadyApplied {
		b.recordSeekApplied(job, "", "", 0, nil)
		log.Info().Msgf("seek: skip — already applied (page indicator): %q @ %s", job.Title, job.Company)
		return false
	}

	score, reasoning, halalVerdict, ok := b.checkSeekScore(ctx, job, details.Description)
	if !ok {
		return false
	}

	resumePath, coverPath := b.generateSeekDocs(ctx, job, details.Description)

	if b.cfg.RequireReview {
		b.saveSeekPendingReview(&domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformSeek,
			Link:                 job.URL,
			ResumePath:           resumePath,
			CoverLetterPath:      coverPath,
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
		// Not a Quick Apply job — queue for manual application via Top Matches.
		b.saveSeekPendingReview(&domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformSeek,
			Link:                 job.URL,
			ResumePath:           resumePath,
			CoverLetterPath:      coverPath,
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

	return b.submitSeekApplication(ctx, br, job, resumePath, coverPath, score, halalVerdict)
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
		log.Info().Msgf("seek: skip — score %d < %d for %q @ %s", result.Score, minScore, job.Title, job.Company)
		return result.Score, result.Reasoning, nil, false
	}
	log.Info().Msgf("seek: score %d/10 — %q @ %s", result.Score, job.Title, job.Company)

	// Halal check — only runs after score passes to avoid wasted LLM calls.
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
			log.Info().Msgf("seek: halal DOUBTFUL — letting through %q @ %s", job.Title, job.Company)
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

	if err := page.WaitLoad(); err != nil {
		return scraper.JobDetails{Description: job.Title + " at " + job.Company, PostedDate: job.PostedDate}
	}
	_ = page.WaitStable(500 * time.Millisecond)

	// Extract company from the detail page — more reliable than listing card selectors.
	if job.Company == "" {
		for _, sel := range []string{
			"[data-automation='advertiser-name']",
			"[data-automation='job-detail-company-name']",
			"[data-automation*='advertiser']",
			"h3[data-automation]",
		} {
			if c, err := page.Element(sel); err == nil {
				if txt, err := c.Text(); err == nil && txt != "" {
					job.Company = txt
					break
				}
			}
		}
	}

	// Check detail page for applied indicator (covers manual applications not yet in our DB).
	if !job.AlreadyApplied {
		appliedSelectors := []string{
			"[data-automation='job-detail-applied-label']",
			"[data-automation*='applied-label']",
			"[data-automation*='already-applied']",
		}
		for _, sel := range appliedSelectors {
			if _, err := page.Element(sel); err == nil {
				job.AlreadyApplied = true
				break
			}
		}
		if !job.AlreadyApplied {
			if btn, err := page.Element("[data-automation='job-detail-apply'], button[data-automation*='apply']"); err == nil {
				if txt, err := btn.Text(); err == nil {
					lower := strings.ToLower(strings.TrimSpace(txt))
					if lower == "applied" || strings.Contains(lower, "you applied") {
						job.AlreadyApplied = true
					}
				}
			}
		}
	}

	// Authoritative Quick Apply detection: Seek-hosted form = Quick Apply; external href = manual apply.
	if !job.EasyApply {
		if btn, err := page.Element("[data-automation='job-detail-apply']"); err == nil {
			if txt, err := btn.Text(); err == nil {
				lower := strings.ToLower(strings.TrimSpace(txt))
				if strings.Contains(lower, "quick apply") || lower == "apply" {
					job.EasyApply = true
				}
			}
			// <button> with no href is always a Seek-hosted form (Quick Apply).
			if href, e := btn.Attribute("href"); e != nil {
				job.EasyApply = true // no href attr = <button> = Quick Apply
			} else if href != nil {
				h := *href
				// Relative or seek.com.au href = Seek-hosted.
				if h == "" || strings.HasPrefix(h, "/") || strings.Contains(h, "seek.com.au") {
					job.EasyApply = true
				}
				// External http(s) link to another domain = manual apply only.
			}
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
		context := jobDesc
		if market != nil && market.CoverLetterPrompt != "" {
			context = market.CoverLetterPrompt + "\n" + jobDesc
		}
		if body, err := b.cfg.Tailor.WriteCoverLetter(ctx, profile, context); err == nil {
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
		log.Info().Msgf("seek: queued for review — %q @ %s", p.Role, p.Company)
	} else {
		log.Info().Msgf("seek: added to Top Matches (manual apply) — %q @ %s", p.Role, p.Company)
	}
}

// ── Application submission ─────────────────────────────────────────────────

func (b *Bot) submitSeekApplication(ctx context.Context, br *rod.Browser, job seekJob, resumePath, coverPath string, score int, halalVerdict []byte) bool {
	jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		log.Error().Err(err).Msg("seek: open job page")
		return false
	}
	defer jobPage.Close()

	if err := b.seekApply(ctx, jobPage, resumePath, coverPath); err != nil {
		log.Error().Err(err).Str("job", job.Title).Msg("seek: apply failed")
		b.recordSeekSkipped(job, "seek apply: "+err.Error(), 0, "", nil)
		return false
	}
	b.recordSeekApplied(job, resumePath, coverPath, score, halalVerdict)
	log.Info().Str("company", job.Company).Str("title", job.Title).Msg("seek: applied ✓")
	return true
}

// seekApply navigates Seek's application form on the given job page.
//
// Flow:
//  1. Find the apply button and check it's a Seek-hosted form (not external ATS)
//  2. Click apply → wait for application form to load
//  3. Upload resume PDF (and cover letter if provided)
//  4. Submit the form
func (b *Bot) seekApply(ctx context.Context, page *rod.Page, resumePath, coverPath string) error {
	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}

	// Find the apply button.
	applyBtn, err := page.Element("[data-automation='job-detail-apply']")
	if err != nil {
		// Fallback selectors
		applyBtn, err = page.Element("a[href*='/apply'], button[data-automation*='apply']")
		if err != nil {
			return fmt.Errorf("apply button not found: %w", err)
		}
	}

	// Check if this is an external application link (non-automatable).
	if href, e := applyBtn.Attribute("href"); e == nil && href != nil {
		h := *href
		if h != "" && !strings.Contains(h, "seek.com.au") && strings.HasPrefix(h, "http") {
			return fmt.Errorf("external application")
		}
	}

	if err := applyBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click apply: %w", err)
	}
	b.humanPause()

	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("wait form load: %w", err)
	}
	_ = page.WaitStable(500 * time.Millisecond)

	// Upload resume if path is provided.
	if resumePath != "" {
		if err := b.seekUploadFile(page, resumePath, "resume"); err != nil {
			log.Warn().Err(err).Msg("seek: resume upload failed")
			// Non-fatal — the user's stored profile resume may be used by default.
		}
	}

	// Upload cover letter if path is provided.
	if coverPath != "" {
		if err := b.seekUploadFile(page, coverPath, "cover"); err != nil {
			log.Warn().Err(err).Msg("seek: cover letter upload failed, continuing without it")
		}
	}

	b.humanPause()

	// Fill any unanswered form questions before submitting.
	b.fillFormStep(ctx, page, resumePath, coverPath)

	// Submit the application.
	submitBtn, err := page.Element("[data-automation='review-submit-button']")
	if err != nil {
		submitBtn, err = page.Element("button[type='submit']")
		if err != nil {
			return fmt.Errorf("submit button not found: %w", err)
		}
	}
	if err := submitBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click submit: %w", err)
	}
	b.humanPause()

	// Verify success — look for a confirmation element or URL change.
	if _, err := page.Element("[data-automation='application-success'], [data-automation='confirmation-page']"); err != nil {
		// Also accept a URL containing "application-confirmation" or "success".
		info, _ := page.Info()
		if !strings.Contains(info.URL, "confirm") && !strings.Contains(info.URL, "success") && !strings.Contains(info.URL, "thank") {
			return fmt.Errorf("could not confirm submission")
		}
	}

	return nil
}

// seekUploadFile finds a file input matching the hint ("resume" or "cover") and uploads the file.
func (b *Bot) seekUploadFile(page *rod.Page, filePath, hint string) error {
	// Try specific selectors first, then generic file inputs.
	selectors := []string{
		fmt.Sprintf("input[type='file'][name*='%s']", hint),
		fmt.Sprintf("input[type='file'][id*='%s']", hint),
		fmt.Sprintf("input[type='file'][accept*='pdf']"),
		"input[type='file']",
	}
	for _, sel := range selectors {
		if input, err := page.Element(sel); err == nil {
			return input.SetFiles([]string{filePath})
		}
	}
	return fmt.Errorf("file input not found for %s", hint)
}

// ── DB helpers (Seek-specific wrappers) ───────────────────────────────────

func (b *Bot) recordSeekApplied(job seekJob, resumePath, coverPath string, score int, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	_, _ = b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	)
}

func (b *Bot) recordSeekSkipped(job seekJob, reason string, score int, reasoning string, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	_, _ = b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformSeek), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	)
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
