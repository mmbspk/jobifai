package recommend

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/user/jobifai/internal/eval/candidate"
)

// Metrics aggregated per candidate for one eval run.
type Metrics struct {
	Candidate       candidate.Spec `json:"candidate"`
	CaseCount       int            `json:"case_count"`
	SuccessRate     float64        `json:"success_rate"`
	DetPassRate     float64        `json:"deterministic_pass_rate"`
	CriticalFails   int            `json:"critical_failures"`
	ScoringFalsePosRate float64    `json:"scoring_false_positive_rate,omitempty"`
	ScoringFalseNegRate float64    `json:"scoring_false_negative_rate,omitempty"`
	MeanLatencyMS   float64        `json:"mean_latency_ms"`
	P90LatencyMS    float64        `json:"p90_latency_ms"`
	TotalCostMicro  int64          `json:"total_cost_usd_micro"`
	MeanCostMicro   float64        `json:"mean_cost_usd_micro"`
	QualityDelta    float64        `json:"quality_delta_vs_baseline"`
	CostDeltaPct    float64        `json:"cost_delta_pct_vs_baseline"`
}

// Outcome recommendation labels.
const (
	OutcomeRecommend              = "recommend"
	OutcomeKeepIncumbent          = "keep_incumbent"
	OutcomeNeedsMoreData          = "needs_more_data"
	OutcomeManualReview           = "manual_review_required"
	OutcomeNotDeployableCrossProv = "not_deployable_cross_provider"
)

// Recommendation is the structured admin-facing result.
type Recommendation struct {
	Task            string          `json:"task"`
	Outcome         string          `json:"outcome"`
	Baseline        candidate.Spec  `json:"baseline"`
	Candidate       candidate.Spec  `json:"candidate"`
	SampleSize      int             `json:"sample_size"`
	Metrics         Metrics         `json:"metrics"`
	Confidence      string          `json:"confidence"`
	Deployable      bool            `json:"deployable"`
	Reason          string          `json:"reason"`
	EstimatedSaving float64         `json:"estimated_savings_pct"`
}

// Decide picks a recommendation comparing baseline vs one candidate.
func Decide(task string, baseline, cand Metrics, minCases int, maxCritical int, minQualityDelta float64, crossProvider bool) Recommendation {
	rec := Recommendation{
		Task:       task,
		Baseline:   baseline.Candidate,
		Candidate:  cand.Candidate,
		SampleSize: cand.CaseCount,
		Metrics:    cand,
	}
	if crossProvider {
		rec.Outcome = OutcomeNotDeployableCrossProv
		rec.Deployable = false
		rec.Reason = "cross-provider production routing not implemented"
		return rec
	}
	if cand.CaseCount < minCases {
		rec.Outcome = OutcomeNeedsMoreData
		rec.Confidence = "low"
		rec.Reason = fmt.Sprintf("sample size %d below minimum %d", cand.CaseCount, minCases)
		return rec
	}
	if cand.CriticalFails > maxCritical {
		rec.Outcome = OutcomeKeepIncumbent
		rec.Reason = fmt.Sprintf("%d critical failures exceed threshold %d", cand.CriticalFails, maxCritical)
		rec.Confidence = "high"
		return rec
	}
	if cand.QualityDelta < minQualityDelta {
		rec.Outcome = OutcomeKeepIncumbent
		rec.Reason = fmt.Sprintf("quality delta %.3f below floor %.3f", cand.QualityDelta, minQualityDelta)
		rec.Confidence = "medium"
		return rec
	}
	if baseline.TotalCostMicro > 0 {
		rec.EstimatedSaving = (1 - float64(cand.TotalCostMicro)/float64(baseline.TotalCostMicro)) * 100
	}
	rec.Outcome = OutcomeRecommend
	rec.Deployable = true
	rec.Confidence = confidenceLabel(cand.CaseCount, cand.DetPassRate)
	rec.Reason = "lowest-cost candidate clearing quality floor"
	return rec
}

func confidenceLabel(n int, passRate float64) string {
	if n >= 80 && passRate >= 0.95 {
		return "high"
	}
	if n >= 30 && passRate >= 0.9 {
		return "medium"
	}
	return "low"
}

// Aggregate builds metrics from per-case rows.
func Aggregate(spec candidate.Spec, passes, successes, critical int, latencies []int64, costs []int64, scoringFN, scoringFP int) Metrics {
	m := Metrics{Candidate: spec, CaseCount: len(latencies)}
	if m.CaseCount == 0 {
		return m
	}
	for _, c := range costs {
		m.TotalCostMicro += c
	}
	m.MeanCostMicro = float64(m.TotalCostMicro) / float64(m.CaseCount)
	m.SuccessRate = float64(successes) / float64(m.CaseCount)
	m.DetPassRate = float64(passes) / float64(m.CaseCount)
	m.CriticalFails = critical
	var sumLat int64
	for _, l := range latencies {
		sumLat += l
	}
	m.MeanLatencyMS = float64(sumLat) / float64(m.CaseCount)
	m.P90LatencyMS = p90(latencies)
	if m.CaseCount > 0 {
		m.ScoringFalseNegRate = float64(scoringFN) / float64(m.CaseCount)
		m.ScoringFalsePosRate = float64(scoringFP) / float64(m.CaseCount)
	}
	return m
}

func p90(v []int64) float64 {
	if len(v) == 0 {
		return 0
	}
	cp := append([]int64(nil), v...)
	for i := 0; i < len(cp); i++ {
		for j := i + 1; j < len(cp); j++ {
			if cp[j] < cp[i] {
				cp[i], cp[j] = cp[j], cp[i]
			}
		}
	}
	idx := int(math.Ceil(0.9*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	return float64(cp[idx])
}

func MustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
