package bot

// shadow_apply.go — shadow mode observation for the adaptive engine.
//
// Shadow mode runs the legacy fill path unchanged (browser actions, form
// navigation, submission) while also running the adaptive StepInterpreter on
// each form step. The interpreter's proposals are logged for comparison against
// legacy behavior without changing any candidate data or submit action.
//
// Invariants:
//   1. Shadow observer must not issue browser actions.
//   2. Shadow observer must not write to the active mapping cache.
//   3. Legacy path is unaffected: fillFormStep fills the form exactly as before.
//
// Wiring: wireShadowObserver sets b.cfg.ShadowStepFn immediately before the
// legacy apply call in manager.go. fillFormStep calls b.cfg.ShadowStepFn after
// scanning fields (before filling) so the snapshot is unmodified.

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// ShadowObservation records one form step as seen by the adaptive interpreter
// during a shadow-mode legacy run. It is not persisted — it exists only for
// in-process comparison logging and in-test assertions.
type ShadowObservation struct {
	Snap           StepSnapshot       `json:"snap"`
	Interpretation StepInterpretation `json:"interpretation"`
	LLMUsed        bool               `json:"llm_used"`
	Err            error              `json:"-"`
}

// wireShadowObserver configures b.cfg.ShadowStepFn so that every fillFormStep
// call during a shadow-mode legacy run also runs the adaptive interpreter.
//
// The observer uses a read-only view of AdaptiveMapping (ReadOnly=true) so
// warm-path lookups still work but Write calls are silent no-ops.
// AdaptiveLLM must be non-nil for the observer to be wired; if it is nil,
// the function is a no-op (shadow still runs but produces no interpreter data).
//
// observations is optional; when non-nil each step result is appended to it.
// This is used by tests to assert interpreter behavior without log parsing.
func wireShadowObserver(b *Bot, ac *AttemptContext, observations *[]ShadowObservation) {
	if b.cfg.AdaptiveLLM == nil {
		log.Warn().Str("attempt_id", safeAttemptID(ac)).Msg("shadow: AdaptiveLLM not set — step observations disabled")
		return
	}

	// Build a read-only mapping view so lookups hit the cache but writes don't.
	var shadowMapping MappingLookup
	if b.cfg.AdaptiveMapping != nil {
		shadowMapping = readOnlyMappingAdapter{b.cfg.AdaptiveMapping}
	}

	si := &StepInterpreter{
		LLM:         b.cfg.AdaptiveLLM,
		Mapping:     shadowMapping,
		Preferences: b.currentPreferences(),
	}
	profile := b.currentProfile()

	b.cfg.ShadowStepFn = func(ctx context.Context, snap StepSnapshot) {
		interp, err := si.InterpretStep(ctx, snap, profile)

		obs := ShadowObservation{
			Snap:           snap,
			Interpretation: interp,
			LLMUsed:        interp.LLMUsed,
			Err:            err,
		}
		if observations != nil {
			*observations = append(*observations, obs)
		}

		if err != nil {
			b.log.Warn().Err(err).
				Str("snap_sig", snap.SemanticSignature()).
				Str("attempt_id", safeAttemptID(ac)).
				Msg("shadow: interpreter error on step observation")
			return
		}

		pendingCount := 0
		for _, r := range interp.Fields {
			if r.Pending {
				pendingCount++
			}
		}
		b.log.Info().
			Str("snap_sig", snap.SemanticSignature()).
			Str("attempt_id", safeAttemptID(ac)).
			Int("fields", len(interp.Fields)).
			Int("pending", pendingCount).
			Bool("llm_used", interp.LLMUsed).
			Msg("shadow: form step observed")

		for _, r := range interp.Fields {
			b.log.Debug().
				Str("ref", r.Ref).
				Str("source", string(r.Source)).
				Str("action", string(r.Action)).
				Bool("pending", r.Pending).
				Str("reason", string(r.Reason)).
				Msg("shadow: field proposal")
		}
		// Shadow NEVER writes to the active mapping cache — the write is intentionally absent.
	}
}

// readOnlyMappingAdapter wraps any MappingLookup for use as a shadow read view.
// The adaptive interpreter accepts MappingLookup (read-only), so no Write path.
type readOnlyMappingAdapter struct {
	inner MappingLookup
}

func (r readOnlyMappingAdapter) LookupField(fieldSig, employerKey string) (FieldResolution, bool) {
	return r.inner.LookupField(fieldSig, employerKey)
}

// safeAttemptID returns the attempt ID from ac, or "" if ac is nil.
func safeAttemptID(ac *AttemptContext) string {
	if ac == nil {
		return ""
	}
	return ac.AttemptID
}

// installShadowObserverIfNeeded wires the shadow observer on b when the engine
// mode is shadow and AdaptiveLLM is available. The observations slice, when
// non-nil, collects per-step results for test assertions.
// Returns false if the observer was not wired (LLM unavailable or not shadow mode).
func installShadowObserverIfNeeded(b *Bot, ac *AttemptContext, observations *[]ShadowObservation) bool {
	if ac == nil || ac.Engine != domain.ApplyEngineShadow {
		return false
	}
	if b.cfg.AdaptiveLLM == nil {
		return false
	}
	wireShadowObserver(b, ac, observations)
	return true
}
