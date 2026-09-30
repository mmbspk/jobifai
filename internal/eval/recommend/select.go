package recommend

import (
	"fmt"

	"github.com/user/jobifai/internal/domain"
)

const OutcomeQualityUpgrade = "quality_upgrade_candidate"

// SelectInput controls joint recommendation across all candidates.
type SelectInput struct {
	Task                string
	Baseline            Metrics
	Candidates          []Metrics
	MinCases            int
	MaxCritical         int
	CrossProvider       bool
	SmokeOrFake         bool
	QualityFloor        func(task string, baseline, cand Metrics) (ok bool, reason string)
	EffectiveSampleSize int
}

// SelectAll returns one recommendation per non-baseline candidate plus marks the best cost saver.
func SelectAll(in SelectInput) []Recommendation {
	var out []Recommendation
	if in.SmokeOrFake {
		for _, c := range in.Candidates {
			out = append(out, Recommendation{
				Task: in.Task, Baseline: in.Baseline.Candidate, Candidate: c.Candidate,
				SampleSize: c.CaseCount, Metrics: c, Outcome: OutcomeNeedsMoreData,
				Deployable: false, Reason: "smoke/fake runs cannot produce deployable recommendations",
			})
		}
		return out
	}
	var eligible []Metrics
	for _, c := range in.Candidates {
		rec := evaluateOne(in, c)
		out = append(out, rec)
		if rec.Deployable && rec.Outcome == OutcomeRecommend {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) == 0 {
		return out
	}
	best := eligible[0]
	for _, c := range eligible[1:] {
		if c.TotalCostMicro < best.TotalCostMicro {
			best = c
		}
	}
	for i := range out {
		if out[i].Outcome != OutcomeRecommend || !out[i].Deployable {
			continue
		}
		if out[i].Candidate.ID() != best.Candidate.ID() {
			out[i].Outcome = OutcomeKeepIncumbent
			out[i].Deployable = false
			out[i].Reason = "another candidate is lower cost at equal quality floor"
		}
	}
	return out
}

func evaluateOne(in SelectInput, cand Metrics) Recommendation {
	rec := Recommendation{
		Task: in.Task, Baseline: in.Baseline.Candidate, Candidate: cand.Candidate,
		SampleSize: cand.CaseCount, Metrics: cand,
	}
	cand.QualityDelta = cand.DetPassRate - in.Baseline.DetPassRate
	if in.Baseline.TotalCostMicro > 0 {
		cand.CostDeltaPct = (float64(cand.TotalCostMicro)/float64(in.Baseline.TotalCostMicro) - 1) * 100
	}
	rec.Metrics = cand
	if cand.Candidate.Provider != in.Baseline.Candidate.Provider {
		rec.Outcome = OutcomeNotDeployableCrossProv
		rec.Reason = "cross-provider production routing not implemented"
		return rec
	}
	if !cand.CoverageComplete {
		rec.Outcome = OutcomeNeedsMoreData
		rec.Reason = "candidate did not complete required case coverage"
		return rec
	}
	effectiveN := in.EffectiveSampleSize
	if effectiveN <= 0 {
		effectiveN = cand.EffectiveSampleSize
	}
	if effectiveN <= 0 {
		effectiveN = cand.UniqueInputCount
	}
	if effectiveN < in.MinCases {
		rec.Outcome = OutcomeNeedsMoreData
		rec.Reason = fmt.Sprintf("effective sample size %d below minimum %d (raw cases %d)", effectiveN, in.MinCases, cand.CaseCount)
		return rec
	}
	if cand.CriticalFails > in.MaxCritical {
		rec.Outcome = OutcomeKeepIncumbent
		rec.Reason = fmt.Sprintf("%d critical failures exceed threshold", cand.CriticalFails)
		return rec
	}
	if in.QualityFloor != nil {
		if ok, reason := in.QualityFloor(in.Task, in.Baseline, cand); !ok {
			rec.Outcome = OutcomeKeepIncumbent
			rec.Reason = reason
			return rec
		}
	}
	if cand.TotalCostMicro > in.Baseline.TotalCostMicro && cand.QualityDelta > 0.05 {
		rec.Outcome = OutcomeQualityUpgrade
		rec.Reason = "quality upgrade candidate; not a cost-saving recommendation"
		return rec
	}
	if cand.TotalCostMicro >= in.Baseline.TotalCostMicro {
		rec.Outcome = OutcomeKeepIncumbent
		rec.Reason = "candidate is not cheaper than baseline"
		return rec
	}
	if in.Baseline.TotalCostMicro > 0 {
		rec.EstimatedSaving = (1 - float64(cand.TotalCostMicro)/float64(in.Baseline.TotalCostMicro)) * 100
	}
	rec.Outcome = OutcomeRecommend
	rec.Deployable = true
	rec.Confidence = confidenceLabel(cand.CaseCount, cand.DetPassRate)
	rec.Reason = "lowest-cost candidate clearing quality floor among evaluated alternatives"
	if in.Task == domain.TaskResumeTailoring || in.Task == domain.TaskCoverLetter {
		rec.Outcome = OutcomeManualReview
		rec.Deployable = false
		rec.Reason = "subjective writing task requires manual review before any production deployment"
	}
	return rec
}

// DefaultQualityFloor applies task-specific gates using extended metrics in Metrics.
func DefaultQualityFloor(task string, baseline, cand Metrics) (bool, string) {
	switch task {
	case "job_scoring":
		if cand.ScoringFalseNegRate > baseline.ScoringFalseNegRate+0.01 {
			return false, "job_scoring false-negative regression"
		}
	case "form_answer":
		if cand.DetPassRate < 0.95 {
			return false, "form_answer deterministic pass rate below 95%"
		}
	case "form_vision":
		if cand.DetPassRate < 0.85 {
			return false, "form_vision pass rate below 85%"
		}
	}
	if cand.DetPassRate+0.02 < baseline.DetPassRate {
		return false, "deterministic pass rate regression"
	}
	return true, ""
}

// Extended scoring metrics on Metrics struct - add fields to recommend.go Metrics