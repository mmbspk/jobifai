package resume

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	reTag      = regexp.MustCompile(`<[^>]+>`)
	reSpaces   = regexp.MustCompile(`[ \t]{2,}`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
)

// FetchJobPage does a best-effort HTTP GET of the given URL and returns the
// visible text content (HTML tags stripped). Returns an error if the page is
// unreachable, requires auth (4xx), or times out.
// It retries once on network/timeout failures so that a cold-start TCP+TLS
// handshake on the first attempt doesn't surface as a user-facing error.
func FetchJobPage(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := range 2 {
		_ = attempt
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Fresh timeout per attempt — cold-start TLS/DNS on attempt 0
		// must not consume the retry budget for attempt 1.
		aCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		text, err := fetchJobPageOnce(aCtx, rawURL)
		cancel()
		if err == nil {
			return text, nil
		}
		lastErr = err
		// HTTP errors (login required, not found) are deliberate — no point retrying.
		if strings.Contains(err.Error(), "HTTP ") {
			break
		}
	}
	return "", lastErr
}

func fetchJobPageOnce(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: page requires login or is unavailable", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	text := stripHTML(string(body))
	if len(text) > 10000 {
		text = text[:10000]
	}
	return text, nil
}

func stripHTML(s string) string {
	// Remove script/style blocks entirely
	for _, tag := range []string{"script", "style", "head"} {
		re := regexp.MustCompile(`(?i)<` + tag + `[^>]*>[\s\S]*?</` + tag + `>`)
		s = re.ReplaceAllString(s, "")
	}
	// Block-level tags → newline
	s = regexp.MustCompile(`(?i)<(br|p|div|li|h[1-6]|tr)[^>]*>`).ReplaceAllString(s, "\n")
	// Strip remaining tags
	s = reTag.ReplaceAllString(s, "")
	// Decode common entities
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ", "&#39;", "'", "&quot;", `"`).Replace(s)
	// Collapse whitespace
	s = reSpaces.ReplaceAllString(s, " ")
	s = reNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
