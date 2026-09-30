package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// SampleStats describes semantic uniqueness of a loaded dataset.
type SampleStats struct {
	RawCaseCount        int            `json:"raw_case_count"`
	UniqueInputCount    int            `json:"unique_input_count"`
	RepetitionCount     int            `json:"repetition_count"`
	EffectiveSampleSize int            `json:"effective_sample_size"`
	FingerprintCounts   map[string]int `json:"fingerprint_counts,omitempty"`
}

// InputFingerprint returns a stable hash of decision-relevant case input.
func InputFingerprint(c Case) string {
	norm := normalizeInputJSON(c.Task, c.Input)
	sum := sha256.Sum256(norm)
	return hex.EncodeToString(sum[:16])
}

func normalizeInputJSON(task string, raw json.RawMessage) []byte {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		return raw
	}
	delete(m, "id")
	stripNoise(m)
	b, _ := json.Marshal(m)
	return b
}

func stripNoise(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if strings.EqualFold(k, "id") || strings.EqualFold(k, "case_id") {
				delete(t, k)
				continue
			}
			stripNoise(val)
		}
	case []any:
		for i := range t {
			stripNoise(t[i])
		}
	}
}

// ComputeSampleStats aggregates fingerprints across cases.
func ComputeSampleStats(cases []Case) SampleStats {
	counts := map[string]int{}
	for _, c := range cases {
		fp := InputFingerprint(c)
		counts[fp]++
	}
	unique := len(counts)
	reps := 0
	for _, n := range counts {
		if n > 1 {
			reps += n - 1
		}
	}
	return SampleStats{
		RawCaseCount:        len(cases),
		UniqueInputCount:    unique,
		RepetitionCount:     reps,
		EffectiveSampleSize: unique,
		FingerprintCounts:   counts,
	}
}

// SortedFingerprints returns fingerprint keys sorted for stable reporting.
func (s SampleStats) SortedFingerprints() []string {
	keys := make([]string, 0, len(s.FingerprintCounts))
	for k := range s.FingerprintCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
