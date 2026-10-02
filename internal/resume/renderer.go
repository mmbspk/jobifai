package resume

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/user/jobifai/internal/domain"
)

// PDFRenderer renders a ResumeProfile (or cover-letter text) to a PDF via
// a headless Chrome page using go-rod.
type PDFRenderer struct {
	styleDir string // directory containing style_*.css files
}

func NewPDFRenderer(styleDir string) *PDFRenderer {
	return &PDFRenderer{styleDir: styleDir}
}

// RenderResume generates a PDF from a ResumeProfile using the named CSS style.
// If styleName is empty the first available style is used (or the built-in default).
// cssOverride, if non-empty, is a direct file path that takes precedence over styleName.
// opts may supply market-specific section headings; nil uses English defaults.
func (r *PDFRenderer) RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string, opts *RenderOptions) ([]byte, error) {
	css, err := r.loadCSS(styleName, cssOverride)
	if err != nil {
		return nil, err
	}
	labels := DefaultSectionLabels()
	if opts != nil {
		labels = opts.SectionLabels
	}
	html, err := renderResumeHTML(profile, css, labels)
	if err != nil {
		return nil, err
	}
	return r.htmlToPDF(ctx, html)
}

// RenderCoverLetter generates a PDF from a cover-letter text body.
func (r *PDFRenderer) RenderCoverLetter(ctx context.Context, body string, styleName, cssOverride string) ([]byte, error) {
	css, err := r.loadCSS(styleName, cssOverride)
	if err != nil {
		return nil, err
	}
	html := renderCoverLetterHTML(body, css)
	return r.htmlToPDF(ctx, html)
}

// ── CSS loading ───────────────────────────────────────────────────────────

// loadCSS returns CSS content. cssOverride is a direct file path (used for
// market-specific stylesheets); styleName is the display name looked up inside
// styleDir. Falls back to the built-in defaultCSS if nothing is found.
func (r *PDFRenderer) loadCSS(styleName, cssOverride string) (string, error) {
	if cssOverride != "" {
		data, err := os.ReadFile(cssOverride)
		if err == nil {
			return expandStylesheetImports(string(data), filepath.Dir(cssOverride)), nil
		}
		// file not found, fall through to style directory lookup
	}
	if r.styleDir == "" {
		return defaultCSS, nil
	}
	entries, err := os.ReadDir(r.styleDir)
	if err != nil {
		return defaultCSS, nil // dir not found, use built-in
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".css")
		name := strings.ReplaceAll(strings.TrimPrefix(stem, "style_"), "_", " ")
		if styleName == "" || strings.EqualFold(name, styleName) {
			data, err := os.ReadFile(filepath.Join(r.styleDir, e.Name()))
			if err != nil {
				return defaultCSS, nil
			}
			return string(data), nil
		}
	}
	return defaultCSS, nil
}

// ── HTML rendering ────────────────────────────────────────────────────────

var resumeTempl = template.Must(
	template.New("resume").
		Funcs(template.FuncMap{
			"join": strings.Join,
			// skillRow bolds the category label (text before first ':') in a skill string.
			// e.g. "Cloud & Infrastructure: AWS, K8s" → "<strong>Cloud & Infrastructure:</strong> AWS, K8s"
			"skillRow": func(s string) template.HTML {
				if idx := strings.Index(s, ":"); idx >= 0 {
					label := template.HTMLEscapeString(s[:idx])
					rest := template.HTMLEscapeString(s[idx+1:])
					return template.HTML("<strong>" + label + ":</strong>" + rest)
				}
				return template.HTML(template.HTMLEscapeString(s))
			},
		}).
		Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><style>{{.CSS}}</style></head>
<body>
<header>
  <h1>{{.P.PersonalInformation.Name}} {{.P.PersonalInformation.Surname}}</h1>
  {{if .P.PersonalInformation.Headline}}<p class="job-title">{{.P.PersonalInformation.Headline}}</p>{{end}}
  <div class="contact-info">
    {{if .P.PersonalInformation.City}}<p>{{.P.PersonalInformation.City}}{{if .P.PersonalInformation.Country}}, {{.P.PersonalInformation.Country}}{{end}}</p>{{end}}
    {{if .P.PersonalInformation.Email}}<p>{{.P.PersonalInformation.Email}}</p>{{end}}
    {{if .P.PersonalInformation.Phone}}<p>{{.P.PersonalInformation.PhonePrefix}}{{.P.PersonalInformation.Phone}}</p>{{end}}
    {{if .P.PersonalInformation.LinkedIn}}<p><a href="{{.P.PersonalInformation.LinkedIn}}">{{.P.PersonalInformation.LinkedIn}}</a></p>{{end}}
    {{if .P.PersonalInformation.GitHub}}<p><a href="{{.P.PersonalInformation.GitHub}}">{{.P.PersonalInformation.GitHub}}</a></p>{{end}}
  </div>
</header>
<main>
  {{if .P.Summary}}<section id="summary"><h2>{{.L.Summary}}</h2><p>{{.P.Summary}}</p></section>{{end}}

  {{if .P.Skills}}<section id="skills-technical"><h2>{{.L.Skills}}</h2>
  <div class="two-column">
    {{range .P.Skills}}<div class="skill-row">{{skillRow .}}</div>{{end}}
  </div></section>{{end}}

  {{if .P.ExperienceDetails}}<section id="work-experience"><h2>{{.L.Experience}}</h2>
  {{range .P.ExperienceDetails}}
  <div class="entry">
    <div class="entry-title-line">{{.Position}}</div>
    <div class="entry-meta-line">{{.Company}}{{if .Location}} · {{.Location}}{{end}}{{if .EmploymentPeriod}} · <em>{{.EmploymentPeriod}}</em>{{end}}</div>
    {{if .KeyResponsibilities}}<ul class="compact-list">{{range .KeyResponsibilities}}<li>{{.}}</li>{{end}}</ul>{{end}}
  </div>
  {{end}}</section>{{end}}

  {{if .P.Projects}}<section id="side-projects"><h2>{{.L.Projects}}</h2>
  {{range .P.Projects}}
  <div class="entry">
    <div class="entry-title-line">{{if .Link}}<a href="{{.Link}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}{{if .Technologies}}<span class="entry-tech"> · {{join .Technologies ", "}}</span>{{end}}</div>
    {{if .Description}}<p class="entry-desc">{{.Description}}</p>{{end}}
  </div>
  {{end}}</section>{{end}}

  {{if .P.Certifications}}<section id="certifications"><h2>{{.L.Certifications}}</h2>
  <ul class="compact-list">
    {{range .P.Certifications}}<li><strong>{{.Name}}</strong>{{if .Issuer}}, {{.Issuer}}{{end}}{{if .Date}}, {{.Date}}{{end}}</li>{{end}}
  </ul></section>{{end}}

  {{if .P.Publications}}<section id="publications"><h2>{{.L.Publications}}</h2>
  <ul class="compact-list">
    {{range .P.Publications}}<li>{{if .Authors}}{{.Authors}}. {{end}}<strong>{{.Title}}</strong>{{if .Journal}}. <em>{{.Journal}}</em>{{end}}{{if .Year}}, {{.Year}}{{end}}{{if .DOI}}. {{.DOI}}{{end}}{{if .Status}} [{{.Status}}]{{end}}</li>{{end}}
  </ul></section>{{end}}

  {{if .P.Presentations}}<section id="presentations"><h2>{{.L.Presentations}}</h2>
  <ul class="compact-list">
    {{range .P.Presentations}}<li>{{if .Year}}{{.Year}}, {{end}}<strong>{{.Title}}</strong>{{if .Conference}}, <em>{{.Conference}}</em>{{end}}{{if .Role}} ({{.Role}}){{end}}</li>{{end}}
  </ul></section>{{end}}

  {{if .P.Grants}}<section id="grants"><h2>{{.L.Grants}}</h2>
  <ul class="compact-list">
    {{range .P.Grants}}<li>{{if .Year}}{{.Year}}, {{end}}<strong>{{.Funder}}</strong>{{if .Project}}: {{.Project}}{{end}}{{if .Amount}} ({{.Amount}}){{end}}</li>{{end}}
  </ul></section>{{end}}

  {{if .P.EducationDetails}}<section id="education"><h2>{{.L.Education}}</h2>
  {{range .P.EducationDetails}}
  <div class="entry">
    <div class="entry-title-line">{{.EducationLevel}}{{if .FieldOfStudy}} in {{.FieldOfStudy}}{{end}}</div>
    <div class="entry-meta-line">{{.Institution}}{{if .StartDate}} · {{.StartDate}} – {{.YearOfCompletion}}{{else}}{{if .YearOfCompletion}} · {{.YearOfCompletion}}{{end}}{{end}}</div>
    {{if .Thesis}}<p class="entry-desc"><em>Thesis: {{.Thesis}}</em></p>{{end}}
  </div>
  {{end}}</section>{{end}}

  {{if .P.Languages}}<section id="skills-languages"><h2>{{.L.Languages}}</h2>
  <p>{{range .P.Languages}}{{.Language}} ({{.Proficiency}})  {{end}}</p>
  </section>{{end}}
</main>
</body></html>`))

func renderResumeHTML(p *domain.ResumeProfile, css string, labels SectionLabels) (string, error) {
	var buf bytes.Buffer
	data := struct {
		P   *domain.ResumeProfile
		L   SectionLabels
		CSS template.CSS
	}{P: p, L: labels, CSS: template.CSS(css)}
	if err := resumeTempl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render resume html: %w", err)
	}
	return buf.String(), nil
}

// ExpandStylesheetImports inlines local @import "./file.css" for PDF rendering.
func ExpandStylesheetImports(css, dir string) string {
	return expandStylesheetImports(css, dir)
}

func expandStylesheetImports(css, dir string) string {
	const lead = `@import "./`
	for {
		idx := strings.Index(css, lead)
		if idx < 0 {
			break
		}
		after := css[idx+len(lead):]
		file, rest, ok := strings.Cut(after, `"`)
		if !ok || file == "" {
			break
		}
		semicolon := strings.Index(rest, ";")
		if semicolon < 0 {
			break
		}
		importEnd := idx + len(lead) + len(file) + 1 + semicolon + 1
		if importEnd > len(css) {
			break
		}
		importStmt := css[idx:importEnd]
		embedded, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			break
		}
		css = strings.Replace(css, importStmt, string(embedded), 1)
	}
	return css
}

func renderCoverLetterHTML(body, css string) string {
	escaped := template.HTMLEscapeString(body)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><style>%s</style></head>
<body><div id="cover-letter"><p>%s</p></div></body></html>`,
		css, strings.ReplaceAll(escaped, "\n", "<br>"))
}

// ── go-rod PDF rendering ──────────────────────────────────────────────────

func (r *PDFRenderer) htmlToPDF(ctx context.Context, html string) ([]byte, error) {
	u, err := launcher.New().Headless(true).Launch()
	if err != nil {
		return nil, fmt.Errorf("launch chrome for pdf: %w", err)
	}
	b := rod.New().ControlURL(u).Context(ctx)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("connect chrome: %w", err)
	}
	defer func() { _ = b.Close() }()

	page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("open page: %w", err)
	}
	if err := page.SetDocumentContent(html); err != nil {
		return nil, fmt.Errorf("set html: %w", err)
	}
	// Wait for layout
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}

	pdf, err := page.PDF(&proto.PagePrintToPDF{
		PrintBackground:   true,
		PreferCSSPageSize: true,
	})
	if err != nil {
		return nil, fmt.Errorf("print pdf: %w", err)
	}
	defer func() { _ = pdf.Close() }()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(pdf); err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// ── built-in minimal CSS (fallback) ──────────────────────────────────────

// DefaultPDFCSS returns the built-in stylesheet used when no market/style override applies.
func DefaultPDFCSS() string { return defaultCSS }

const defaultCSS = `
body { font-family: 'Segoe UI', Arial, sans-serif; font-size: 11pt; color: #222; margin: 0; padding: 2cm; }
h1 { font-size: 22pt; margin-bottom: 2px; }
h2 { font-size: 13pt; border-bottom: 1px solid #ccc; padding-bottom: 3px; margin-top: 18px; }
.contact-info p { margin-right: 12px; font-size: 10pt; color: #555; display: inline; }
.entry { margin-bottom: 10px; }
.entry-header { display: flex; justify-content: space-between; }
.entry-details { display: flex; justify-content: space-between; }
.entry-year { color: #777; font-size: 10pt; }
.compact-list { padding-left: 1.4rem; }
.compact-list li { margin-bottom: 3px; }
a { color: #1a6fc4; text-decoration: none; }
`
