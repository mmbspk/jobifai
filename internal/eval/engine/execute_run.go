package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/preflight"
	"github.com/user/jobifai/internal/eval/recommend"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/eval/validators"
)

func (s *Service) runCases(ctx context.Context, runID string, run runRow, bundle dataset.Bundle) (finalStatus string, summary map[string]any, persistErr error) {
	baseline := run.Baseline
	if baseline.Model == "" {
		baseline = candidate.Spec{Provider: run.BaselineProvider, Model: run.BaselineModel, Effort: run.BaselineEffort}
	}
	all := append([]candidate.Spec{baseline}, run.Candidates...)
	sampleStats := dataset.ComputeSampleStats(bundle.Cases)
	expectedPerCandidate := len(bundle.Cases)
	candReg := newCandidateRegistry(all, expectedPerCandidate)
	gate := newModelAvailability()
	budget := newRunBudget(run.MaxBudgetMicro)
	synthetic := len(bundle.Cases) > 0 && (bundle.Manifest.Classification == "synthetic" || bundle.Cases[0].Classification == "synthetic")

	concurrency := s.Config.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var completed int
	aggregates := map[string]*acc{}
	for _, spec := range all {
		aggregates[spec.ID()] = &acc{spec: spec}
	}

	for _, spec := range all {
		wg.Add(1)
		sem <- struct{}{}
		go func(spec candidate.Spec) {
			defer wg.Done()
			defer func() { <-sem }()
			specID := spec.ID()

			if st, blocked := gate.get(spec.Provider, spec.Model); blocked {
				candReg.setStatus(specID, CandidateUnavailableModel, st.Reason, st.ProviderErrorCode)
				return
			}
			if run.RunnerType == runmeta.RunnerReal && s.EvalPricing != nil {
				_, meta, pErr := s.EvalPricing.ResolveEvalPricing(ctx, spec.Provider, spec.Model, spec.Model)
				if pErr != nil || !meta.Resolved {
					reason := meta.ResolveError
					if pErr != nil && reason == "" {
						reason = pErr.Error()
					}
					candReg.setStatus(specID, CandidatePricingUnresolved, reason, "")
					return
				}
			}

			for _, c := range bundle.Cases {
				if ctx.Err() != nil || budget.shouldStop() {
					return
				}
				if s.cancelRequested(ctx, runID) {
					budget.requestStop()
					return
				}
				if budget.overBudget() {
					budget.requestStop()
					return
				}

				var reserve int64
				if run.RunnerType == runmeta.RunnerReal && s.EvalPricing != nil {
					reserve = s.perCallBudgetReserve(ctx, run.Task, spec, c)
					if reserve > 0 && !budget.tryReserve(reserve) {
						return
					}
				}

				out, obs, runErr := s.Run.RunCase(ctx, run.Task, spec, c)
				if runErr != nil {
					st := preflight.ClassifyProviderError(runErr)
					if st.State == preflight.StateUnavailableModel {
						gate.set(spec.Provider, spec.Model, st)
						candReg.setStatus(specID, CandidateUnavailableModel, st.Reason, st.ProviderErrorCode)
						return
					}
				}
				vr := validators.Validate(run.Task, out, c.Expect, c.Critical)
				if runErr != nil {
					vr.Errors = append(vr.Errors, runErr.Error())
				}
				pricingStop := run.RunnerType == runmeta.RunnerReal && obs.Success && !obs.PricingResolved &&
					obs.Tokens.InputTokens+obs.Tokens.OutputTokens > 0
				if pricingStop {
					if vr.Metrics == nil {
						vr.Metrics = map[string]any{}
					}
					vr.Metrics["candidate_status"] = preflight.StatePricingUnresolved
					candReg.setStatus(specID, CandidatePricingUnresolved, obs.PricingError, "")
				}

				mu.Lock()
				if persistErr != nil {
					mu.Unlock()
					return
				}
				mu.Unlock()

				outText := ""
				if synthetic {
					outText = out
				}
				insErr := s.insertResultExtended(context.WithoutCancel(ctx), runID, spec, c, obs, runErr == nil, vr, outText)
				mu.Lock()
				if insErr != nil {
					persistErr = insErr
					mu.Unlock()
					return
				}
				charge := obs.BudgetChargeMicro
				if charge <= 0 && obs.PricingResolved {
					charge = obs.RawCostMicro
				}
				if run.RunnerType == runmeta.RunnerReal && reserve > 0 {
					budget.reconcile(reserve, obs.RawCostMicro, charge)
				} else if charge > 0 || obs.PricingResolved {
					budget.charge(obs.RawCostMicro, charge)
				}
				if pricingStop {
					completed++
					mu.Unlock()
					return
				}
				completed++
				candReg.addCompleted(specID)
				a := aggregates[specID]
				a.n++
				if runErr == nil {
					a.successes++
				}
				if vr.Pass {
					a.passes++
				}
				if vr.CriticalFail {
					a.critical++
				}
				switch vr.Metrics["scoring_cell"] {
				case "tp":
					a.scoringTP++
				case "tn":
					a.scoringTN++
				case "fp":
					a.scoringFP++
				case "fn":
					a.scoringFN++
				case "borderline":
					a.scoringBorderline++
					if vr.Metrics["predicted_pass"] == true {
						a.scoringBorderlineSurfaced++
					}
				}
				a.costs = append(a.costs, obs.RawCostMicro)
				a.latencies = append(a.latencies, obs.LatencyMS)
				mu.Unlock()
			}
			candReg.finalizeRunnable(specID)
		}(spec)
	}
	wg.Wait()

	rawSpent, budgetSpent, stopped := budget.totals()
	for _, spec := range all {
		candReg.finalizeRunnable(spec.ID())
	}

	summary = map[string]any{
		"spent_micro": rawSpent, "budget_spent_micro": budgetSpent,
		"cases": len(bundle.Cases), "completed": completed,
		"sample_stats": sampleStats, "candidates": candReg.snapshot(),
	}

	finalStatus = runmeta.StatusCompleted
	if ctx.Err() != nil || s.cancelRequested(ctx, runID) {
		finalStatus = runmeta.StatusCancelled
	} else if stopped && run.MaxBudgetMicro > 0 && budgetSpent >= run.MaxBudgetMicro {
		finalStatus = runmeta.StatusBudgetExhausted
	} else if ok, reason := candReg.benchmarkComplete(run.Purpose); !ok && run.Purpose == runmeta.PurposeBenchmark {
		finalStatus = runmeta.StatusFailed
		summary["completion_error"] = reason
	}

	if persistErr != nil {
		return finalStatus, summary, persistErr
	}
	if err := s.writeRecommendations(ctx, runID, run, baseline, all, aggregates, candReg, sampleStats, finalStatus, rawSpent, summary); err != nil {
		return finalStatus, summary, err
	}
	return finalStatus, summary, nil
}

func (s *Service) writeRecommendations(ctx context.Context, runID string, run runRow, baseline candidate.Spec, all []candidate.Spec,
	aggregates map[string]*acc, candReg *candidateRegistry, sampleStats dataset.SampleStats,
	finalStatus string, rawSpent int64, summary map[string]any) error {

	baseM := metricsFromAcc(aggregates[baseline.ID()])
	baseM.CoverageComplete = candidateCoverageComplete(candReg, baseline.ID())
	baseM.RawCaseCount = sampleStats.RawCaseCount
	baseM.UniqueInputCount = sampleStats.UniqueInputCount
	baseM.TotalUniqueInputCount = sampleStats.TotalUniqueInputCount
	baseM.EffectiveSampleSize = sampleStats.EffectiveSampleSize
	baseM.HardEffectiveSampleSize = sampleStats.HardEffectiveSampleSize
	baseM.BorderlineEffectiveSampleSize = sampleStats.BorderlineEffectiveSampleSize
	baseM.RepetitionCount = sampleStats.RepetitionCount

	var candMetrics []recommend.Metrics
	for _, spec := range all[1:] {
		m := metricsFromAcc(aggregates[spec.ID()])
		m.CoverageComplete = candidateCoverageComplete(candReg, spec.ID())
		m.RawCaseCount = sampleStats.RawCaseCount
		m.UniqueInputCount = sampleStats.UniqueInputCount
		m.TotalUniqueInputCount = sampleStats.TotalUniqueInputCount
		m.EffectiveSampleSize = sampleStats.EffectiveSampleSize
		m.HardEffectiveSampleSize = sampleStats.HardEffectiveSampleSize
		m.BorderlineEffectiveSampleSize = sampleStats.BorderlineEffectiveSampleSize
		m.RepetitionCount = sampleStats.RepetitionCount
		candMetrics = append(candMetrics, m)
	}

	smokeOrFake := run.RunnerType != runmeta.RunnerReal || run.Purpose != runmeta.PurposeBenchmark
	minCases := runmeta.MinBenchmarkCases(run.Task)
	if smokeOrFake {
		minCases = 1_000_000
	}
	recs := recommend.SelectAll(recommend.SelectInput{
		Task: run.Task, Baseline: baseM, Candidates: candMetrics,
		MinCases: minCases, MaxCritical: 0,
		SmokeOrFake: smokeOrFake, QualityFloor: recommend.DefaultQualityFloor,
		EffectiveSampleSize:           sampleStats.EffectiveSampleSize,
		HardEffectiveSampleSize:         sampleStats.HardEffectiveSampleSize,
		BorderlineEffectiveSampleSize: sampleStats.BorderlineEffectiveSampleSize,
	})
	if finalStatus != runmeta.StatusCompleted {
		for i := range recs {
			recs[i].Deployable = false
			if recs[i].Outcome == recommend.OutcomeRecommend {
				recs[i].Outcome = recommend.OutcomeManualReview
				recs[i].Reason = fmt.Sprintf("run ended with status %s; not deployable", finalStatus)
			}
		}
	}
	persist := context.WithoutCancel(ctx)
	for _, rec := range recs {
		if err := s.insertRecommendation(persist, runID, rec); err != nil {
			return fmt.Errorf("recommendation persist: %w", err)
		}
	}
	sumJSON, _ := json.Marshal(summary)
	_, err := s.DB.ExecContext(persist, `
		UPDATE model_eval_runs SET status=?, completed_at=CURRENT_TIMESTAMP,
			actual_cost_usd_micro=?, summary_json=? WHERE id=?`,
		finalStatus, rawSpent, string(sumJSON), runID)
	return err
}

func candidateCoverageComplete(reg *candidateRegistry, id string) bool {
	for _, rec := range reg.snapshot() {
		if rec.ID == id {
			return rec.Status == CandidateCompleted && rec.CompletedCases >= rec.ExpectedCases
		}
	}
	return false
}

func (r *candidateRegistry) benchmarkComplete(purpose string) (bool, string) {
	if purpose != runmeta.PurposeBenchmark {
		return true, ""
	}
	for _, rec := range r.snapshot() {
		switch rec.Status {
		case CandidateUnavailableModel, CandidateMissingCredential, CandidateCapabilityMismatch, CandidatePricingUnresolved:
			continue
		case CandidateCompleted:
			if rec.CompletedCases < rec.ExpectedCases {
				return false, fmt.Sprintf("candidate %s incomplete (%d/%d)", rec.ID, rec.CompletedCases, rec.ExpectedCases)
			}
		default:
			return false, fmt.Sprintf("candidate %s status %s", rec.ID, rec.Status)
		}
	}
	return true, ""
}
