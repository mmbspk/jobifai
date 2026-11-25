package bot

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-rod/rod"
)

// A screenshot label cannot establish the owning control, required status,
// options, or surrounding qualifiers. Keep selector failures operational;
// asking the candidate for another fact cannot repair browser identity.
var errUnresolvedLegacyControl = errors.New("application form control could not be bound to a current verified answer plan")

// The historical vision fill used substring targeting and unverified scalar
// answers. With answer reviews enabled, try one full DOM materialization/rescan
// instead. Only current, uniquely bound controls can use the shared answer plan
// and exact-use recorder; screenshot predictions never grant fill authority.
func (b *Bot) fillRecoveredLegacyControls(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (filled bool, err error) {
	if page == nil || lazy == nil {
		return false, errUnresolvedLegacyControl
	}
	fields, currentPage, err := b.scanAdaptiveStep(ctx, page)
	if err != nil {
		return false, err
	}
	if len(fields) == 0 || currentPage == nil {
		return false, errUnresolvedLegacyControl
	}
	for _, field := range fields {
		if field.Type == "file" || (field.DocumentKind != "" && field.DocumentKind != "unknown") {
			return false, fmt.Errorf("%w: document controls require the normal scoped upload path", errUnresolvedLegacyControl)
		}
	}
	if err := b.prepareLegacyAnswers(ctx, lazy, fields); err != nil {
		return false, err
	}
	for _, field := range fields {
		resolution, ok := lazy.answerPlan[legacyFieldKey(field)]
		current, found := lazy.answerSnapshot.FieldByRef(resolution.Ref)
		if !ok || !found || resolution.Pending {
			return filled, errUnresolvedLegacyControl
		}
		if resolution.Action == ActionSkip {
			continue
		}
		if resolution.Action != ActionFill || resolution.Value == "" {
			return filled, errUnresolvedLegacyControl
		}
		if err := b.ensureAnswerFactsCurrent(lazy); err != nil {
			return filled, err
		}
		if err := b.checkAdaptiveSideEffect(); err != nil {
			return filled, err
		}
		result := fillAdaptiveControl(ctx, currentPage, current, resolution.Value, b.currentProfile())
		if result.Err != nil {
			return filled, result.Err
		}
		if !result.Filled {
			return filled, errUnresolvedLegacyControl
		}
		filled = true
		if err := b.markLegacyAnswerFilled(ctx, lazy, field, resolution.Value); err != nil {
			return filled, err
		}
	}
	return filled, nil
}
