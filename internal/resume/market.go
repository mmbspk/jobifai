package resume

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const sharedATSPromptRel = "prompts/_shared/ats_json.en.txt"

// MarketPrompts holds locale metadata and prompt prefixes for a target market.
type MarketPrompts struct {
	Name              string `yaml:"name"`
	Locale            string `yaml:"locale"`            // BCP 47, e.g. en-US (future UI i18n)
	DocumentLanguage  string `yaml:"document_language"` // ISO 639-1 primary output language
	RegionGroup       string `yaml:"region_group"`      // americas | europe | asia_pacific | global
	PageSize          string `yaml:"page_size"`         // a4 | letter
	CSSFile           string `yaml:"css_file"`
	SectionLabels     SectionLabels `yaml:"section_labels"`
	ResumePrompt      string `yaml:"resume_prompt"`
	TailoredPrompt    string `yaml:"tailored_prompt"`
	CoverLetterPrompt string `yaml:"cover_letter_prompt"`
}

type promptFiles struct {
	Resume   string `yaml:"resume"`
	Tailored string `yaml:"tailored"`
	Cover    string `yaml:"cover"`
}

type marketDocument struct {
	MarketPrompts `yaml:",inline"`
	PromptFiles   promptFiles `yaml:"prompt_files"`
}

// LoadMarket reads a market YAML file, optional prompt_files, and appends shared ATS rules.
func LoadMarket(path string) (*MarketPrompts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc marketDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	m := doc.MarketPrompts
	marketDir := filepath.Dir(path)
	if err := loadPromptFileRefs(&m, marketDir, doc.PromptFiles); err != nil {
		return nil, err
	}
	appendSharedATSRules(&m, marketDir)
	normalizeMarketMeta(&m)
	return &m, nil
}

func normalizeMarketMeta(m *MarketPrompts) {
	if m.DocumentLanguage == "" {
		m.DocumentLanguage = "en"
	}
	if m.Locale == "" {
		m.Locale = "en"
	}
}

func appendSharedATSRules(m *MarketPrompts, marketDir string) {
	data, err := os.ReadFile(filepath.Join(marketDir, sharedATSPromptRel))
	if err != nil {
		return
	}
	suffix := "\n\n" + strings.TrimSpace(string(data))
	for _, p := range []*string{&m.ResumePrompt, &m.TailoredPrompt, &m.CoverLetterPrompt} {
		if strings.TrimSpace(*p) != "" {
			*p = strings.TrimSpace(*p) + suffix
		}
	}
}

func loadPromptFileRefs(m *MarketPrompts, marketDir string, refs promptFiles) error {
	if refs.Resume != "" && strings.TrimSpace(m.ResumePrompt) == "" {
		text, err := readMarketFile(marketDir, refs.Resume)
		if err != nil {
			return err
		}
		m.ResumePrompt = text
	}
	if refs.Tailored != "" && strings.TrimSpace(m.TailoredPrompt) == "" {
		text, err := readMarketFile(marketDir, refs.Tailored)
		if err != nil {
			return err
		}
		m.TailoredPrompt = text
	}
	if refs.Cover != "" && strings.TrimSpace(m.CoverLetterPrompt) == "" {
		text, err := readMarketFile(marketDir, refs.Cover)
		if err != nil {
			return err
		}
		m.CoverLetterPrompt = text
	}
	return nil
}

func readMarketFile(marketDir, rel string) (string, error) {
	path := rel
	if !filepath.IsAbs(rel) {
		path = filepath.Join(marketDir, rel)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadMarketByName searches marketDir for a YAML file whose `name` field
// matches (case-insensitive) and returns its prompts. Returns nil if not found.
func LoadMarketByName(marketDir, name string) *MarketPrompts {
	if marketDir == "" || name == "" {
		return nil
	}
	entries, err := os.ReadDir(marketDir)
	if err != nil {
		return nil
	}
	nameLower := filepath.Base(name)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") {
			continue
		}
		m, err := LoadMarket(filepath.Join(marketDir, e.Name()))
		if err != nil {
			continue
		}
		if equalFold(m.Name, nameLower) || equalFold(e.Name(), name) {
			return m
		}
	}
	return nil
}

func equalFold(a, b string) bool {
	return strings.EqualFold(a, b)
}
