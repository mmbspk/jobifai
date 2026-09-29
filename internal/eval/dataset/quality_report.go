package dataset

import (
	"encoding/json"
	"fmt"
)

// QualityReport summarizes semantic diversity for a dataset bundle.
type QualityReport struct {
	TotalCases        int
	UniqueInputs      int
	DuplicateRatio    float64
	ScenarioTags      map[string]int
	CriticalCount     int
	ExpectPassCount   int
	ExpectSkipCount   int
}

// BuildQualityReport inspects a loaded bundle.
func BuildQualityReport(b Bundle) QualityReport {
	r := QualityReport{TotalCases: len(b.Cases), ScenarioTags: map[string]int{}}
	seen := map[string]int{}
	for _, c := range b.Cases {
		if c.Critical {
			r.CriticalCount++
		}
		fp := inputFingerprint(c.Input)
		seen[fp]++
		for _, tag := range c.Tags {
			r.ScenarioTags[tag]++
		}
		var exp map[string]any
		_ = json.Unmarshal(c.Expect, &exp)
		if exp["expect_pass"] == true {
			r.ExpectPassCount++
		}
		if exp["expect_skip"] == true {
			r.ExpectSkipCount++
		}
	}
	r.UniqueInputs = len(seen)
	if r.TotalCases > 0 {
		dup := 0
		for _, n := range seen {
			if n > 1 {
				dup += n - 1
			}
		}
		r.DuplicateRatio = float64(dup) / float64(r.TotalCases)
	}
	return r
}

// AssertMinimumDiversity fails when a dataset collapses to too few scenario templates.
func AssertMinimumDiversity(b Bundle, minTemplates int) error {
	r := BuildQualityReport(b)
	if len(r.ScenarioTags) > 0 {
		if len(r.ScenarioTags) < minTemplates {
			return fmt.Errorf("only %d scenario templates, need >= %d", len(r.ScenarioTags), minTemplates)
		}
		return nil
	}
	if r.DuplicateRatio > 0.5 && b.Manifest.Task != "form_vision" {
		return fmt.Errorf("duplicate input ratio %.2f too high", r.DuplicateRatio)
	}
	return nil
}
