package resume

// SectionLabels are PDF section headings. Defaults are English; markets override via YAML for future locale packs.
type SectionLabels struct {
	Summary       string `yaml:"summary"`
	Skills        string `yaml:"skills"`
	Experience    string `yaml:"experience"`
	Projects      string `yaml:"projects"`
	Certifications string `yaml:"certifications"`
	Publications  string `yaml:"publications"`
	Presentations string `yaml:"presentations"`
	Grants        string `yaml:"grants"`
	Education     string `yaml:"education"`
	Languages     string `yaml:"languages"`
}

// DefaultSectionLabels returns English headings matching the canonical HTML template.
func DefaultSectionLabels() SectionLabels {
	return SectionLabels{
		Summary:        "Professional Summary",
		Skills:         "Technical Expertise",
		Experience:     "Work Experience",
		Projects:       "Projects",
		Certifications: "Certifications",
		Publications:   "Publications",
		Presentations:  "Conference Presentations",
		Grants:         "Research Grants & Funding",
		Education:      "Education",
		Languages:      "Languages",
	}
}

// LabelsOrDefault merges optional market labels onto English defaults.
func (m *MarketPrompts) LabelsOrDefault() SectionLabels {
	out := DefaultSectionLabels()
	if m == nil {
		return out
	}
	merge := func(dst *string, src string) {
		if src != "" {
			*dst = src
		}
	}
	merge(&out.Summary, m.SectionLabels.Summary)
	merge(&out.Skills, m.SectionLabels.Skills)
	merge(&out.Experience, m.SectionLabels.Experience)
	merge(&out.Projects, m.SectionLabels.Projects)
	merge(&out.Certifications, m.SectionLabels.Certifications)
	merge(&out.Publications, m.SectionLabels.Publications)
	merge(&out.Presentations, m.SectionLabels.Presentations)
	merge(&out.Grants, m.SectionLabels.Grants)
	merge(&out.Education, m.SectionLabels.Education)
	merge(&out.Languages, m.SectionLabels.Languages)
	return out
}

// RenderOptions optional PDF rendering overrides (market section headings, etc.).
type RenderOptions struct {
	SectionLabels SectionLabels
}
