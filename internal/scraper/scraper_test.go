package scraper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── extractText ───────────────────────────────────────────────────────────────

func TestExtractText_Basic(t *testing.T) {
	text := extractText(`<html><body><p>Hello World</p></body></html>`)
	assert.Contains(t, text, "Hello World")
}

func TestExtractText_StripsScripts(t *testing.T) {
	text := extractText(`<html><body><script>var x = 1;</script><p>Visible</p></body></html>`)
	assert.Contains(t, text, "Visible")
	assert.NotContains(t, text, "var x")
}

func TestExtractText_StripsStyles(t *testing.T) {
	text := extractText(`<html><head><style>.foo{color:red}</style></head><body><p>Content</p></body></html>`)
	assert.Contains(t, text, "Content")
	assert.NotContains(t, text, "color")
}

func TestExtractText_StripsNoscript(t *testing.T) {
	text := extractText(`<html><body><noscript>enable js</noscript><p>Main</p></body></html>`)
	assert.Contains(t, text, "Main")
	assert.NotContains(t, text, "enable js")
}

func TestExtractText_MultipleElements(t *testing.T) {
	text := extractText(`<html><body><h1>Title</h1><p>Paragraph one.</p><p>Paragraph two.</p></body></html>`)
	assert.Contains(t, text, "Title")
	assert.Contains(t, text, "Paragraph one.")
	assert.Contains(t, text, "Paragraph two.")
}

// ── extractJSONLDDate ─────────────────────────────────────────────────────────

func TestExtractJSONLDDate_ValidThrough(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"JobPosting","validThrough":"2026-06-30"}</script>`
	assert.Equal(t, "2026-06-30", extractJSONLDDate(html))
}

func TestExtractJSONLDDate_ApplicationDeadline(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"JobPosting","applicationDeadline":"2026-07-01"}</script>`
	assert.Equal(t, "2026-07-01", extractJSONLDDate(html))
}

func TestExtractJSONLDDate_ValidThroughTakesPrecedence(t *testing.T) {
	html := `<script type="application/ld+json">{"validThrough":"2026-06-30","applicationDeadline":"2026-08-01"}</script>`
	assert.Equal(t, "2026-06-30", extractJSONLDDate(html))
}

func TestExtractJSONLDDate_NotFound(t *testing.T) {
	assert.Equal(t, "", extractJSONLDDate(`<html><body><p>No JSON-LD here</p></body></html>`))
}

func TestExtractJSONLDDate_InvalidJSON(t *testing.T) {
	assert.Equal(t, "", extractJSONLDDate(`<script type="application/ld+json">not valid json</script>`))
}

func TestExtractJSONLDDate_NoDateFields(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"JobPosting","title":"Analyst"}</script>`
	assert.Equal(t, "", extractJSONLDDate(html))
}

func TestExtractJSONLDDate_EmptyDateValue(t *testing.T) {
	html := `<script type="application/ld+json">{"validThrough":""}</script>`
	assert.Equal(t, "", extractJSONLDDate(html))
}

// ── extractPostedDate ─────────────────────────────────────────────────────────

func TestExtractPostedDate_Found(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"JobPosting","datePosted":"2026-04-01"}</script>`
	assert.Equal(t, "2026-04-01", extractPostedDate(html))
}

func TestExtractPostedDate_NotFound(t *testing.T) {
	assert.Equal(t, "", extractPostedDate(`<html><body>nothing</body></html>`))
}

func TestExtractPostedDate_EmptyValue(t *testing.T) {
	html := `<script type="application/ld+json">{"datePosted":""}</script>`
	assert.Equal(t, "", extractPostedDate(html))
}

// ── extractTextDate ───────────────────────────────────────────────────────────

func TestExtractTextDate_ApplyBy(t *testing.T) {
	html := `<html><body><p>Apply by May 15, 2026 to be considered.</p></body></html>`
	assert.Equal(t, "May 15, 2026", extractTextDate(html))
}

func TestExtractTextDate_Closes(t *testing.T) {
	html := `<html><body><p>Applications close: 01 June 2026</p></body></html>`
	assert.Equal(t, "01 June 2026", extractTextDate(html))
}

func TestExtractTextDate_Deadline(t *testing.T) {
	html := `<html><body><p>Deadline: 30/06/2026</p></body></html>`
	assert.Equal(t, "30/06/2026", extractTextDate(html))
}

func TestExtractTextDate_NotFound(t *testing.T) {
	assert.Equal(t, "", extractTextDate(`<html><body><p>No deadline mentioned.</p></body></html>`))
}

// ── ParseHTML (exported integration) ─────────────────────────────────────────

func TestParseHTML_ExtractsDescriptionAndDates(t *testing.T) {
	html := `<html><body>
		<p>Exciting opportunity for an experienced analyst in our growing team.</p>
		<script type="application/ld+json">{"@type":"JobPosting","validThrough":"2026-08-01","datePosted":"2026-05-01"}</script>
	</body></html>`
	d := ParseHTML(html)
	assert.Contains(t, d.Description, "Exciting opportunity")
	assert.Equal(t, "2026-08-01", d.DueDate)
	assert.Equal(t, "2026-05-01", d.PostedDate)
}

func TestParseHTML_EmptyDates(t *testing.T) {
	html := `<html><body><p>Short description text here for a junior role.</p></body></html>`
	d := ParseHTML(html)
	assert.Contains(t, d.Description, "Short description")
	assert.Empty(t, d.DueDate)
	assert.Empty(t, d.PostedDate)
}

// ── FetchJob (HTTP integration) ───────────────────────────────────────────────

func TestFetchJob_ReturnsDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>This is a detailed posting for a senior analyst role with extensive responsibilities across multiple departments in a large organisation.</p></body></html>`))
	}))
	t.Cleanup(srv.Close)

	details, err := FetchJob(t.Context(), srv.URL)
	require.NoError(t, err)
	assert.Contains(t, details.Description, "senior analyst")
}

func TestFetchJob_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchJob(t.Context(), srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestFetchJob_TextTooShort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>Too short.</p></body></html>`))
	}))
	t.Cleanup(srv.Close)

	_, err := FetchJob(t.Context(), srv.URL)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "meaningful text"))
}

func TestFetchJob_IncludesDates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
			<p>We are seeking a motivated individual for our team. This role involves planning, coordination, and delivery across multiple business units. The ideal candidate brings strong communication skills.</p>
			<script type="application/ld+json">{"@type":"JobPosting","validThrough":"2026-09-01","datePosted":"2026-05-07"}</script>
		</body></html>`))
	}))
	t.Cleanup(srv.Close)

	details, err := FetchJob(t.Context(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-01", details.DueDate)
	assert.Equal(t, "2026-05-07", details.PostedDate)
}
