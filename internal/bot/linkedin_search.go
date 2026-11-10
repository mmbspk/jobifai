package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/user/jobifai/internal/browser"
)

var errLinkedInSearchUnauthenticated = errors.New("linkedin jobs search requires a confirmed logged-in session")

type linkedInSearchJobJSON struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Company        string `json:"company"`
	Location       string `json:"location"`
	URL            string `json:"url"`
	EasyApply      bool   `json:"easyApply"`
	AlreadyApplied bool   `json:"alreadyApplied"`
}

func (b *Bot) ensureLinkedInSearchSession(page *rod.Page) error {
	if err := b.linkedinEnsureLoggedIn(page); err != nil {
		return fmt.Errorf("%w: %v", errLinkedInSearchUnauthenticated, err)
	}
	state, err := browser.PageLoginState(page, "linkedin")
	if err != nil {
		return fmt.Errorf("%w: login probe failed: %v", errLinkedInSearchUnauthenticated, err)
	}
	if state == browser.LoginStateYes {
		return nil
	}
	if state == browser.LoginStateUnknown && linkedInSearchPageAuthenticated(page) {
		return nil
	}
	if state == browser.LoginStateNo {
		return fmt.Errorf("%w: state=%s (log in via Settings or the automation browser, then retry)", errLinkedInSearchUnauthenticated, state)
	}
	return fmt.Errorf("%w: state=%s (log in via Settings or the automation browser, then retry)", errLinkedInSearchUnauthenticated, state)
}

func linkedInSearchPageAuthenticated(page *rod.Page) bool {
	val, err := page.Timeout(5 * time.Second).Eval(jsLinkedInSearchAuthenticated)
	return err == nil && val != nil && val.Value.Bool()
}

func parseLinkedInSearchJobsJSON(raw string, fallbackLocation string) []linkedInJob {
	var parsed []linkedInSearchJobJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	out := make([]linkedInJob, 0, len(parsed))
	for _, j := range parsed {
		if j.ID == "" {
			continue
		}
		loc := j.Location
		if loc == "" {
			loc = fallbackLocation
		}
		url := j.URL
		if url == "" {
			url = "https://www.linkedin.com/jobs/view/" + j.ID + "/"
		}
		out = append(out, linkedInJob{
			ID:             j.ID,
			Company:        strings.TrimSpace(j.Company),
			Title:          deduplicateTitle(j.Title),
			Location:       strings.TrimSpace(loc),
			URL:            url,
			AlreadyApplied: j.AlreadyApplied,
			EasyApply:      j.EasyApply,
		})
	}
	return out
}

func fetchLinkedInSearchJobs(page *rod.Page, fallbackLocation string) []linkedInJob {
	val, err := page.Timeout(15 * time.Second).Eval(jsLinkedInSearchJobs)
	if err == nil && val != nil {
		if jobs := parseLinkedInSearchJobsJSON(val.Value.String(), fallbackLocation); len(jobs) > 0 {
			return jobs
		}
	}
	return collectLinkedInJobsFromElements(page, fallbackLocation)
}

func collectLinkedInJobsFromElements(page *rod.Page, fallbackLocation string) []linkedInJob {
	cards := fetchLinkedInCardsRod(page)
	seen := map[string]bool{}
	var jobs []linkedInJob
	for _, card := range cards {
		job := extractLinkedInJob(card)
		if job.Location == "" {
			job.Location = fallbackLocation
		}
		if job.ID != "" && !seen[job.ID] {
			seen[job.ID] = true
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func fetchLinkedInCardsRod(page *rod.Page) rod.Elements {
	cards, err := page.Elements(".job-card-container")
	if err != nil || len(cards) == 0 {
		cards, _ = page.Elements("[data-job-id], [data-occludable-job-id]")
	}
	return cards
}

// searchKeywordPause waits between keyword searches to mimic reading result pages.
func (b *Bot) searchKeywordPause() {
	min := b.cfg.Settings.HumanBehavior.JobReadTimeMin
	if min == 0 {
		min = 5
	}
	max := b.cfg.Settings.HumanBehavior.JobReadTimeMax
	if max <= min {
		max = min + 5
	}
	d := time.Duration(min+rand.Intn(max-min+1)) * time.Second
	time.Sleep(d)
}
