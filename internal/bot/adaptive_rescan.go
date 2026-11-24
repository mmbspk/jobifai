package bot

import (
	"context"

	"github.com/go-rod/rod"
)

func interpretationUploads(interp StepInterpretation) bool {
	for _, field := range interp.Fields {
		if field.Action == ActionUpload {
			return true
		}
	}
	return false
}

// Semantic proof plus a verified current fill can be learned even when a later
// field or navigation needs work. This does not cache unverified proposals or
// grant any navigation/upload/submit authority. Returned refs were successfully
// passed to the store and must not be written again at a confirmed transition.
func writeVerifiedFilledStep(ctx context.Context, store MappingReadWriter, snap StepSnapshot, interp StepInterpretation, result StepExecResult) []string {
	if store == nil {
		return nil
	}
	var verified []FieldResolution
	for _, field := range resolvedFilledRefs(interp.Fields, result.FilledRefs) {
		if field.Provenance == ProvenanceVerifiedByModel && field.grounded != nil && !field.Pending && field.Action == ActionFill {
			verified = append(verified, field)
		}
	}
	if len(verified) == 0 || store.Write(ctx, snap, verified) != nil {
		return nil
	}
	refs := make([]string, len(verified))
	for i, field := range verified {
		refs[i] = field.Ref
	}
	return refs
}

// Confirmed transitions may additionally learn trusted profile bindings. Skip
// grounded fills already written for this execution, while retaining a retry
// when their earlier cache write failed. The confirmed-transition gate remains
// in writeConfirmedStep.
func writeConfirmedRemainingStep(ctx context.Context, store MappingReadWriter, snap StepSnapshot, interp StepInterpretation, result StepExecResult, submissionConfirmed bool, verifiedRefs []string) {
	alreadyWritten := make(map[string]struct{}, len(verifiedRefs))
	for _, ref := range verifiedRefs {
		alreadyWritten[ref] = struct{}{}
	}
	remaining := result
	remaining.FilledRefs = make([]string, 0, len(result.FilledRefs))
	for _, ref := range result.FilledRefs {
		if _, written := alreadyWritten[ref]; !written {
			remaining.FilledRefs = append(remaining.FilledRefs, ref)
		}
	}
	writeConfirmedStep(ctx, store, snap, interp, remaining, submissionConfirmed)
}

// A failed Next click may reveal a genuinely new conditional control. Only a
// changed fresh structure can request another interpretation; applicant values
// and ordinary validation failures cannot trigger an unchanged click loop.
func (b *Bot) newAdaptiveControls(ctx context.Context, page *rod.Page, previous StepSnapshot, job linkedInJob) ([]formField, *rod.Page, bool, error) {
	fields, active, err := b.scanAdaptiveStep(ctx, page)
	if err != nil || active == nil {
		return nil, nil, false, err
	}
	next := captureStepSnapshot(fields, nil, previous.Platform, previous.StepIndex, previous.StepTitle)
	scopeJobDependentFields(&next, job)
	if len(next.Fields) != len(previous.Fields) {
		return fields, active, true, nil
	}
	signatures := map[string]int{}
	for _, field := range previous.Fields {
		signatures[field.FieldSignature(previous.Platform)]++
	}
	for _, field := range next.Fields {
		key := field.FieldSignature(previous.Platform)
		if signatures[key] == 0 {
			return fields, active, true, nil
		}
		signatures[key]--
	}
	return fields, active, false, nil
}
