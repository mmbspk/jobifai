package resume

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarketPrompts holds the three prompt prefixes for a target market.
type MarketPrompts struct {
	Name              string `yaml:"name"`
	CSSFile           string `yaml:"css_file"`
	ResumePrompt      string `yaml:"resume_prompt"`
	TailoredPrompt    string `yaml:"tailored_prompt"`
	CoverLetterPrompt string `yaml:"cover_letter_prompt"`
}

// LoadMarket reads a market YAML file and returns its prompts.
func LoadMarket(path string) (*MarketPrompts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m MarketPrompts
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
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
