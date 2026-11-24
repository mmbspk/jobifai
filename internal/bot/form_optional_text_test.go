package bot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/quota"
)

func TestLegacyOptionalText_MissingMiddleNameDoesNotBlockRequiredContact(t *testing.T) {
	for _, provider := range []string{"not configured", "empty response", "review response"} {
		t.Run(provider, func(t *testing.T) {
			page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="middle">Middle name</label><input id="middle"></div>
 <div data-test-form-element><label for="first">First name</label><input id="first" required></div>
 <button>Continue</button></div></body></html>`)
			profile := basicProfile()
			profile.PersonalInformation.Name = "Casey"
			b := New(Config{Profile: profile})
			if provider != "not configured" {
				tailor := &legacyAnswerTailor{}
				if provider == "review response" {
					tailor.err = fmt.Errorf("no supported middle name: %w", errAdaptiveQuestionReview)
				}
				b.cfg.Tailor = tailor
			}
			lazy := &lazyDocGen{b: b, job: linkedInJob{ID: "optional-text-fixture"}}
			filled, hasFields, err := b.fillFormStep(context.Background(), page, lazy)
			require.NoError(t, err)
			require.True(t, filled)
			require.True(t, hasFields)
			require.True(t, shouldAdvanceApplyForm(err))
			state, evalErr := page.Eval(`() => ({middle: document.getElementById('middle').value, first: document.getElementById('first').value})`)
			require.NoError(t, evalErr)
			require.Empty(t, state.Value.Get("middle").String())
			require.Equal(t, "Casey", state.Value.Get("first").String())
		})
	}
}

func TestLegacyOptionalText_RequiredMiddleNameStillHolds(t *testing.T) {
	page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="middle">Middle name</label><input id="middle" required></div>
 <div data-test-form-element><label for="first">First name</label><input id="first" required></div>
 <button>Continue</button></div></body></html>`)
	b := New(Config{Profile: basicProfile()})
	filled, hasFields, err := b.fillFormStep(context.Background(), page, &lazyDocGen{b: b, job: linkedInJob{ID: "required-text-fixture"}})
	require.ErrorIs(t, err, errAdaptiveQuestionReview)
	require.False(t, filled)
	require.True(t, hasFields)
	require.False(t, shouldAdvanceApplyForm(err))
	state, evalErr := page.Eval(`() => [...document.querySelectorAll('input')].map(el => el.value).join('')`)
	require.NoError(t, evalErr)
	require.Empty(t, state.Value.String())
}

func TestLegacyOptionalText_SupportedOptionalContactIsFilled(t *testing.T) {
	page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="first">First name</label><input id="first"></div>
 <button>Continue</button></div></body></html>`)
	profile := basicProfile()
	profile.PersonalInformation.Name = "Casey"
	tailor := &legacyAnswerTailor{err: errors.New("profile contact should not call the provider")}
	b := New(Config{Profile: profile, Tailor: tailor})
	filled, hasFields, err := b.fillFormStep(context.Background(), page, &lazyDocGen{b: b, job: linkedInJob{ID: "known-optional-fixture"}})
	require.NoError(t, err)
	require.True(t, filled)
	require.True(t, hasFields)
	require.Zero(t, tailor.calls.Load())
	state, evalErr := page.Eval(`() => document.getElementById('first').value`)
	require.NoError(t, evalErr)
	require.Equal(t, "Casey", state.Value.String())
}

func TestLegacyOptionalText_OperationalErrorsStillStop(t *testing.T) {
	providerErr := errors.New("fixture provider failure")
	browserErr := errors.New("fixture browser closed")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"cancellation", context.Canceled},
		{"deadline", context.DeadlineExceeded},
		{"quota", quota.ErrExceeded},
		{"provider", providerErr},
		{"browser", browserErr},
		{"review joined with cancellation", errors.Join(errAdaptiveQuestionReview, context.Canceled)},
		{"review joined with quota", errors.Join(errAdaptiveQuestionReview, quota.ErrExceeded)},
		{"review joined with browser error", errors.Join(errAdaptiveQuestionReview, browserErr)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="middle">Middle name</label><input id="middle"></div>
 <button>Continue</button></div></body></html>`)
			b := New(Config{Profile: basicProfile(), Tailor: &legacyAnswerTailor{err: tc.err}})
			filled, hasFields, err := b.fillFormStep(context.Background(), page, &lazyDocGen{b: b, job: linkedInJob{ID: "failed-optional-fixture"}})
			require.ErrorIs(t, err, tc.err)
			require.False(t, filled)
			require.True(t, hasFields)
			require.False(t, shouldAdvanceApplyForm(err))
			state, evalErr := page.Eval(`() => document.getElementById('middle').value`)
			require.NoError(t, evalErr)
			require.Empty(t, state.Value.String())
		})
	}
}

func TestLegacyOptionalText_CanceledContextCannotBecomeAnOptionalSkip(t *testing.T) {
	page := rodTestPage(t, `<html><body><div class="jobs-easy-apply-modal">
 <div data-test-form-element><label for="middle">Middle name</label><input id="middle"></div>
 <button>Continue</button></div></body></html>`)
	b := New(Config{Profile: basicProfile()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	filled, hasFields, err := b.fillFormStep(ctx, page, &lazyDocGen{b: b, job: linkedInJob{ID: "canceled-optional-fixture"}})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, filled)
	require.True(t, hasFields)
	require.False(t, shouldAdvanceApplyForm(err))
}
