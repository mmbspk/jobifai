package bot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type intentBoundaryFiller struct {
	*stubFiller
	dispatch func(func() error) error
}

func (f *intentBoundaryFiller) ClickSubmitWithIntent(_ context.Context, persistIntent func() error) error {
	return f.dispatch(persistIntent)
}

func TestStepExec_SubmitIntentBoundary(t *testing.T) {
	for _, mode := range []string{"preflight rejected", "success", "lost acknowledgement", "persist rejected", "missing persist"} {
		t.Run(mode, func(t *testing.T) {
			db := openMappingDB(t)
			ac := newAttemptContext("owner", "seek", "12345678", "https://au.seek.com/job/12345678", "Coordinator", "Acme", "fixture", "run")
			beginAttempt(db, ac)
			guard := &SubmitGuard{DB: db, AttemptID: ac.AttemptID, UserID: ac.UserID}
			if mode == "persist rejected" {
				guard.UserID = "other-owner"
			}
			filler := &intentBoundaryFiller{stubFiller: &stubFiller{submitReady: true}}
			filler.dispatch = func(persistIntent func() error) error {
				if mode == "preflight rejected" {
					return errEngineModeReverted
				}
				if mode == "missing persist" {
					return nil
				}
				if err := persistIntent(); err != nil {
					return err
				}
				filler.submitClicks++
				if mode == "lost acknowledgement" {
					return errors.New("browser connection lost after dispatch")
				}
				return nil
			}
			result, err := NewStepExecutor(basicBounds(), guard).ExecuteStep(context.Background(), StepSnapshot{}, StepInterpretation{}, filler)
			var stage string
			require.NoError(t, db.QueryRow(`SELECT stage FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&stage))
			if mode == "success" || mode == "lost acknowledgement" {
				require.NoError(t, err)
				assert.Equal(t, StepOutcomeSubmitted, result.Outcome)
				assert.Equal(t, "submit_dispatch", stage)
				assert.Equal(t, 1, filler.submitClicks)
				assert.ErrorIs(t, guard.PersistIntent(), ErrNoActiveAttempt, "durable intent still blocks a duplicate worker")
				if mode == "lost acknowledgement" {
					assert.Error(t, result.Err, "post-intent errors remain unknown")
				}
			} else {
				require.Error(t, err)
				assert.Equal(t, StepOutcomeError, result.Outcome)
				assert.NotEqual(t, "submit_dispatch", stage)
				assert.Zero(t, filler.submitClicks)
			}
		})
	}
}

func TestNativeSubmit_PreflightBeforeIntentAndFinalDispatchGuard(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		for _, mode := range []string{"allowed", "preflight rejected", "post-intent rejected"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				container := `<dialog open><div data-sdui-screen="EasyApply">%s</div></dialog>`
				if platform == "seek" {
					container = `<form data-testid="quick-apply-form">%s</form>`
				}
				page := rodTestPage(t, `<html><body>`+fmt.Sprintf(container, `<button type="button" id="submit">Submit application</button>`)+`<script>window.clicks=0;document.getElementById('submit').onclick=()=>window.clicks++;</script></body></html>`)
				db := openMappingDB(t)
				ac := newAttemptContext("owner", platform, "12345678", "https://example.com/job/12345678", "Coordinator", "Acme", "fixture", "run")
				beginAttempt(db, ac)
				calls := 0
				beforeClick := func(context.Context) error {
					calls++
					var stage string
					require.NoError(t, db.QueryRow(`SELECT stage FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&stage))
					if calls <= 2 {
						require.NotEqual(t, "submit_dispatch", stage, "preflight completes before durable intent")
					} else {
						require.Equal(t, "submit_dispatch", stage, "dispatch guards recheck after a potentially contended write")
					}
					if (mode == "preflight rejected" && calls == 2) || (mode == "post-intent rejected" && calls == 3) {
						return errEngineModeReverted
					}
					return nil
				}
				var filler FieldFiller = &linkedInRodFiller{page: page, bot: newTestBot(t), beforeClick: beforeClick}
				if platform == "seek" {
					filler = &seekRodFiller{page: page, beforeClick: beforeClick}
				}
				result, err := NewStepExecutor(basicBounds(), &SubmitGuard{DB: db, AttemptID: ac.AttemptID, UserID: ac.UserID}).ExecuteStep(context.Background(), StepSnapshot{}, StepInterpretation{}, filler)
				var stage string
				require.NoError(t, db.QueryRow(`SELECT stage FROM apply_attempts WHERE id=?`, ac.AttemptID).Scan(&stage))
				clicks, evalErr := page.Eval(`()=>window.clicks`)
				require.NoError(t, evalErr)
				switch mode {
				case "preflight rejected":
					require.Equal(t, 2, calls)
					require.ErrorIs(t, err, errEngineModeReverted)
					assert.Equal(t, StepOutcomeError, result.Outcome)
					assert.NotEqual(t, "submit_dispatch", stage)
					assert.Zero(t, clicks.Value.Int())
				case "post-intent rejected":
					require.Equal(t, 3, calls)
					require.NoError(t, err)
					assert.ErrorIs(t, result.Err, errEngineModeReverted)
					assert.Equal(t, StepOutcomeSubmitted, result.Outcome)
					assert.Equal(t, "submit_dispatch", stage)
					assert.Zero(t, clicks.Value.Int(), "a post-intent hold still dispatches nothing")
				default:
					require.Equal(t, 3, calls)
					require.NoError(t, err)
					assert.Equal(t, StepOutcomeSubmitted, result.Outcome)
					assert.Equal(t, "submit_dispatch", stage)
					assert.Equal(t, 1, clicks.Value.Int())
				}
			})
		}
	}
}

func TestNativeSubmit_IntentWriteCannotAuthorizeChangedControl(t *testing.T) {
	for _, platform := range []string{"linkedin", "seek"} {
		for _, replacement := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replacement=%t", platform, replacement), func(t *testing.T) {
				container := `<dialog open><div data-sdui-screen="EasyApply">%s</div></dialog>`
				if platform == "seek" {
					container = `<form data-testid="quick-apply-form">%s</form>`
				}
				page := rodTestPage(t, `<html><body>`+fmt.Sprintf(container, `<button type="button" id="submit">Submit application</button>`)+`<script>window.clicks=0;document.getElementById('submit').onclick=()=>window.clicks++;</script></body></html>`)
				db := openMappingDB(t)
				ac := newAttemptContext("owner", platform, "12345678", "https://example.com/job/12345678", "Coordinator", "Acme", "fixture", "run")
				beginAttempt(db, ac)
				guard := &SubmitGuard{DB: db, AttemptID: ac.AttemptID, UserID: ac.UserID}
				persistCalls := 0
				persistIntent := func() error {
					persistCalls++
					if err := guard.PersistIntent(); err != nil {
						return err
					}
					mutation := `()=>document.getElementById('submit').textContent='Continue'`
					if replacement {
						mutation = `()=>{const old=document.getElementById('submit'),fresh=old.cloneNode(true);fresh.onclick=()=>window.clicks++;old.replaceWith(fresh);}`
					}
					_, err := page.Eval(mutation)
					return err
				}
				var err error
				if platform == "linkedin" {
					err = clickLinkedInAdaptiveSubmitWithIntent(context.Background(), page, nil, persistIntent)
				} else {
					err = clickSeekAdaptiveActionWithIntent(context.Background(), page, true, nil, persistIntent)
				}
				require.Error(t, err)
				assert.Equal(t, 1, persistCalls, "a post-intent control change never retries dispatch preparation")
				clicks, err := page.Eval(`()=>window.clicks`)
				require.NoError(t, err)
				assert.Zero(t, clicks.Value.Int())
				var stage string
				require.NoError(t, db.QueryRow("SELECT stage FROM apply_attempts WHERE id=?", ac.AttemptID).Scan(&stage))
				assert.Equal(t, "submit_dispatch", stage, "ambiguous post-intent state retains its duplicate protection")
			})
		}
	}
}
