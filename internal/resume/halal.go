package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

type halalData struct {
	Title       string
	Company     string
	Description string
}

var halalTempl = template.Must(template.New("halal").Parse(`You are an Islamic finance and employment ethics advisor. Your role is to evaluate whether a given job is halal (permissible) or haram (impermissible) according to mainstream Sunni Islamic scholarship.

## Your Task
Evaluate the provided job title, description, and/or company context, then return a structured verdict.

## Evaluation Criteria
Classify the job as HALAL, HARAM, or DOUBTFUL (mashbooh) based on the following:

**Clearly HARAM — automatic disqualification if the role directly involves:**
- Riba (interest/usury): e.g. structuring loans, setting interest rates, selling interest-based financial products
- Alcohol: production, distribution, sales, or promotion
- Pork or non-halal meat: production, processing, or sales
- Gambling or betting: platforms, odds-setting, operations
- Adult entertainment or pornography
- Weapons of mass destruction or clearly offensive military arms
- Witchcraft, astrology sold as guidance, or occult services

**DOUBTFUL — flag for the user's own judgment if:**
- The role is in a mixed-industry company (e.g. a logistics manager at a brewery — indirect involvement)
- The role touches conventional finance but not directly riba (e.g. a software engineer at a bank)
- The role involves music, entertainment, or media in a grey area
- Significant uncertainty exists about what the role actually entails

**HALAL by default if:**
- The role involves permissible goods or services
- Any connection to haram is remote or incidental

## Output Format
Respond ONLY with a valid JSON object in this exact structure:

{
  "verdict": "HALAL" | "HARAM" | "DOUBTFUL",
  "confidence": "HIGH" | "MEDIUM" | "LOW",
  "summary": "<one sentence plain-English verdict>",
  "reasons": ["<reason 1>", "<reason 2>"],
  "caveats": "<any nuance, scholarly disagreement, or conditions the user should know — or null if none>",
  "scholar_note": "<brief note on which Islamic ruling or principle applies — or null>"
}

Job title: {{.Title}}
Company: {{.Company}}
Job description:
{{.Description}}`))

// HalalChecker evaluates job permissibility under Islamic employment ethics.
type HalalChecker struct {
	client *llm.Client
}

func NewHalalChecker(client *llm.Client) *HalalChecker {
	return &HalalChecker{client: client}
}

// CheckHalal evaluates whether the given job is halal, haram, or doubtful.
func (h *HalalChecker) CheckHalal(ctx context.Context, title, company, description string) (domain.HalalVerdict, error) {
	var prompt bytes.Buffer
	if err := halalTempl.Execute(&prompt, halalData{
		Title:       title,
		Company:     company,
		Description: description,
	}); err != nil {
		return domain.HalalVerdict{}, fmt.Errorf("halal: render prompt: %w", err)
	}

	raw, err := h.client.Chat(ctx, []llm.Message{{Role: "user", Content: prompt.String()}})
	if err != nil {
		return domain.HalalVerdict{}, fmt.Errorf("halal: llm: %w", err)
	}

	raw = stripJSON(raw)
	var verdict domain.HalalVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return domain.HalalVerdict{}, fmt.Errorf("halal: parse: %w\nraw: %s", err, raw)
	}
	return verdict, nil
}
