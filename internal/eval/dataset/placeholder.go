package dataset

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateCaseContent rejects placeholder-style eval cases.
func ValidateCaseContent(c Case) error {
	if c.Task == "" {
		return fmt.Errorf("case %s: missing task", c.ID)
	}
	var in map[string]any
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return fmt.Errorf("case %s: invalid input json", c.ID)
	}
	if len(in) == 0 {
		return fmt.Errorf("case %s: empty input", c.ID)
	}
	if onlyPlaceholderInput(in) {
		return fmt.Errorf("case %s: placeholder input", c.ID)
	}
	var exp map[string]any
	if err := json.Unmarshal(c.Expect, &exp); err != nil {
		return fmt.Errorf("case %s: invalid expect json", c.ID)
	}
	if exp["non_empty"] == true && len(exp) == 1 {
		return fmt.Errorf("case %s: placeholder non_empty expectation", c.ID)
	}
	switch c.Task {
	case "resume_extract":
		if strings.TrimSpace(stringField(in, "resume_text")) == "" {
			return fmt.Errorf("case %s: resume_text required", c.ID)
		}
	case "resume_tailoring", "cover_letter":
		if in["profile"] == nil {
			return fmt.Errorf("case %s: profile required", c.ID)
		}
		if strings.TrimSpace(stringField(in, "job_description")) == "" {
			return fmt.Errorf("case %s: job_description required", c.ID)
		}
	case "application_questions":
		if in["profile"] == nil {
			return fmt.Errorf("case %s: profile required", c.ID)
		}
		if _, ok := in["questions"]; !ok {
			return fmt.Errorf("case %s: questions required", c.ID)
		}
	case "form_vision":
		if strings.TrimSpace(stringField(in, "fixture_png")) == "" {
			return fmt.Errorf("case %s: fixture_png required", c.ID)
		}
		if exp["fields"] == nil {
			return fmt.Errorf("case %s: expect.fields required", c.ID)
		}
	}
	return nil
}

func onlyPlaceholderInput(in map[string]any) bool {
	allowed := map[string]bool{"case_index": true, "task": true, "note": true, "placeholder": true}
	if len(in) == 0 {
		return true
	}
	for k := range in {
		if !allowed[k] {
			return false
		}
	}
	return true
}

func stringField(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
