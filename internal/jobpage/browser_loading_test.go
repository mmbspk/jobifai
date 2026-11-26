package jobpage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserVisiblePartialDescriptionCannotCompleteWhileLoading(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target := fixtureTarget()
	page, _ := readerFixture(t, browser, target, fixtureHTML("<section data-testid='job-description' aria-busy='true'>"+fixtureText()+"</section>"))
	snapshot, err := page.Capture(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	require.True(t, snapshot.Loading)
	_, err = page.Known(t.Context(), target, DefaultLimits())
	require.Equal(t, FailureThin, ErrorCode(err))
	_, err = page.Read(t.Context(), target, snapshot, fixturePlan(snapshot), DefaultLimits())
	require.Equal(t, FailureThin, ErrorCode(err))
	modelCalls := 0
	limits := DefaultLimits()
	limits.MaxRecoveryActions = 1
	reader := NewReader(nil, func(context.Context) (Config, error) { return Config{Recovery: ModeOn, Limits: limits}, nil }, func(context.Context, Limits) (Model, error) { modelCalls++; return &orchestrationModel{}, nil })
	_, err = reader.Read(t.Context(), page, target)
	require.Equal(t, FailureThin, ErrorCode(err))
	require.Zero(t, modelCalls)
}

func TestBrowserCompleteDescriptionMayStartWithLoadingWord(t *testing.T) {
	browser := readerFixtureBrowser(t)
	target := fixtureTarget()
	page, _ := readerFixture(t, browser, target, fixtureHTML("<section data-testid='job-description'>Loading and unloading equipment is part of the role. "+fixtureText()+"</section>"))
	snapshot, err := page.Capture(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	require.False(t, snapshot.Loading)
	result, err := page.Known(t.Context(), target, DefaultLimits())
	require.NoError(t, err)
	require.Contains(t, result.Description, "Loading and unloading")
}

func TestBrowserTrustedBaselineRechecksOwnedLoadingState(t *testing.T) {
	browser := readerFixtureBrowser(t)
	for _, test := range []struct {
		name string
		html string
		code FailureCode
	}{
		{"owned description busy", fixtureHTML("<section data-testid='job-description' aria-busy='true'>" + fixtureText() + "</section>"), FailureThin},
		{"owned scope busy", "<main data-job-id='123' aria-busy='true'><h1>Engineer</h1><a aria-label='Company, Acme.'>Acme</a><section data-testid='job-description'>" + fixtureText() + "</section></main>", FailureThin},
		{"unrelated spinner", fixtureHTML("<section data-testid='job-description'>" + fixtureText() + "</section><aside><div role='progressbar'>Loading</div></aside>"), ""},
		{"complete description", fixtureHTML("<section data-testid='job-description'>" + fixtureText() + "</section>"), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := fixtureTarget()
			page, _ := readerFixture(t, browser, target, test.html)
			page.baseline.Known = func(context.Context, Target, Limits) (Result, error) {
				return Result{Identity: Identity{JobID: target.JobID, Title: target.Title, Company: target.Company, Verified: true}, Description: fixtureText()}, nil
			}
			result, err := page.Known(t.Context(), target, DefaultLimits())
			require.Equal(t, test.code, ErrorCode(err))
			if test.code == "" {
				require.NoError(t, err)
				require.Equal(t, SourceDeterministic, result.Source)
			} else {
				require.Error(t, err)
			}
		})
	}
}
