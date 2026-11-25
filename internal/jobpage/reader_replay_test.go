package jobpage

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type replacedReplayPage struct {
	*orchestrationPage
	replacement Snapshot
	replaced    bool
}

func (p *replacedReplayPage) Read(ctx context.Context, t Target, s Snapshot, plan Plan, l Limits) (Result, error) {
	if !p.replaced {
		p.replaced = true
		p.snapshot = p.replacement
		return Result{}, &ReadError{Code: FailureStale}
	}
	return p.orchestrationPage.Read(ctx, t, s, plan, l)
}
func TestReaderStaleReplayRebindsWithoutInvalidatingRecipe(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	fault := &failingRecipeStore{RecipeStore: store}
	model := &orchestrationModel{}
	reader := NewReader(fault, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return model, nil })
	cold := readSnapshot("123", "A complete current role with supported engineering responsibilities.")
	_, e := reader.Read(context.Background(), &orchestrationPage{snapshot: cold}, cold.Target)
	require.NoError(t, e)
	warm := readSnapshot("456", "A different current role with current engineering responsibilities.")
	fresh := warm
	fresh.ID = "replacement-epoch"
	page := &replacedReplayPage{orchestrationPage: &orchestrationPage{snapshot: warm}, replacement: fresh}
	result, e := reader.Read(context.Background(), page, warm.Target)
	require.NoError(t, e)
	require.Equal(t, SourceRecipe, result.Source)
	require.Equal(t, fresh.Nodes[1].Text, result.Description)
	require.Equal(t, 1, model.proposes)
	rows, e := store.ListRecipes(context.Background(), RecipeActive, 100)
	require.NoError(t, e)
	require.Len(t, rows, 1)
}
