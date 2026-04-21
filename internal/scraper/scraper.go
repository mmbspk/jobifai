// Package scraper fetches a job-posting page and extracts the plain-text
// job description for use by the LLM.
package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// JobDetails holds the extracted job description, optional due date, and posting date.
type JobDetails struct {
	Description string
	DueDate     string // empty if not found on the page
	PostedDate  string // empty if not found on the page
}

// FetchJob fetches jobURL and returns the visible text content plus any
// application due date found in the page (via JSON-LD or text patterns).
func FetchJob(ctx context.Context, jobURL string) (JobDetails, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
	if err != nil {
		return JobDetails{}, fmt.Errorf("scraper: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return JobDetails{}, fmt.Errorf("scraper: fetch %s: %w", jobURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return JobDetails{}, fmt.Errorf("scraper: HTTP %d for %s", resp.StatusCode, jobURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MB cap
	if err != nil {
		return JobDetails{}, fmt.Errorf("scraper: read body: %w", err)
	}

	rawHTML := string(body)
	text := extractText(rawHTML)
	if len(strings.TrimSpace(text)) < 100 {
		return JobDetails{}, fmt.Errorf("scraper: could not extract meaningful text from %s", jobURL)
	}
	return JobDetails{
		Description: text,
		DueDate:     extractDueDate(rawHTML),
		PostedDate:  extractPostedDate(rawHTML),
	}, nil
}

// FetchJobDescription fetches jobURL and returns only the visible text content.
// Deprecated: use FetchJob to also capture due date.
func FetchJobDescription(ctx context.Context, jobURL string) (string, error) {
	d, err := FetchJob(ctx, jobURL)
	return d.Description, err
}

// ParseHTML extracts JobDetails from raw HTML already fetched by the caller
// (e.g. via a rod browser page that bypasses bot detection).
func ParseHTML(rawHTML string) JobDetails {
	text := extractText(rawHTML)
	return JobDetails{
		Description: text,
		DueDate:     extractDueDate(rawHTML),
		PostedDate:  extractPostedDate(rawHTML),
	}
}

// extractDueDate attempts to find an application deadline in the raw HTML.
// It tries JSON-LD structured data first, then falls back to text patterns.
// Returns "" if nothing is found.
func extractDueDate(rawHTML string) string {
	// 1. JSON-LD: Schema.org JobPosting uses "validThrough" for application deadline.
	//    LinkedIn and some other boards embed this.
	if d := extractJSONLDDate(rawHTML); d != "" {
		return d
	}

	// 2. Regex fallback on visible text patterns.
	return extractTextDate(rawHTML)
}

var jsonLDPattern = regexp.MustCompile(`(?i)<script[^>]+type=["']application/ld\+json["'][^>]*>([\s\S]*?)</script>`)

func extractJSONLDDate(rawHTML string) string {
	matches := jsonLDPattern.FindAllStringSubmatch(rawHTML, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &obj); err != nil {
			continue
		}
		for _, key := range []string{"validThrough", "applicationDeadline"} {
			if v, ok := obj[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// extractPostedDate returns the job's original posting date from JSON-LD (Schema.org datePosted).
func extractPostedDate(rawHTML string) string {
	matches := jsonLDPattern.FindAllStringSubmatch(rawHTML, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &obj); err != nil {
			continue
		}
		if v, ok := obj["datePosted"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// datePatternRe matches common "apply by / closes / deadline" phrases followed by a date.
var datePatternRe = regexp.MustCompile(`(?i)(?:apply\s+by|application\s+closes?|closes?|deadline)[:\s]+([A-Za-z]+ \d{1,2},? \d{4}|\d{1,2} [A-Za-z]+ \d{4}|\d{1,2}/\d{1,2}/\d{2,4})`)

func extractTextDate(rawHTML string) string {
	// Run pattern over plain text to avoid matching HTML attribute noise.
	text := extractText(rawHTML)
	m := datePatternRe.FindStringSubmatch(text)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractText parses HTML and returns only the visible text nodes.
func extractText(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML // fallback to raw
	}

	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "head", "meta", "link":
				return
			}
		}
		if n.Type == html.TextNode {
			t := strings.TrimFunc(n.Data, unicode.IsSpace)
			if t != "" {
				sb.WriteString(t)
				sb.WriteByte('\n')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return sb.String()
}
