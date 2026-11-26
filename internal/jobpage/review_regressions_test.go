package jobpage

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestInvisibleDuplicateDoesNotInvalidateWarmRecipe(t *testing.T) {
	snapshot, _, _, _, recipe := recipeFixture(t)
	store, _ := recipeStoreFixture(t)
	_, err := store.SaveVerified(t.Context(), recipe)
	require.NoError(t, err)
	duplicate := snapshot.Nodes[1]
	duplicate.Ref, duplicate.Visible = "n2", false
	snapshot.Nodes = append(snapshot.Nodes, duplicate)
	snapshot.Candidates = append(snapshot.Candidates, duplicate.Ref)
	snapshot.LayoutSignature = StructuralSignature(snapshot)
	page := &orchestrationPage{snapshot: snapshot}
	reader := NewReader(store, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
		t.Fatal("a hidden copy must not trigger another model request")
		return nil, errors.New("unexpected model")
	})
	result, err := reader.Read(t.Context(), page, snapshot.Target)
	require.NoError(t, err)
	require.Equal(t, SourceRecipe, result.Source)
	require.Equal(t, snapshot.Nodes[1].Text, result.Description)
	require.Zero(t, result.Metrics.HTTPAttempts)
	stored, err := store.GetRecipe(t.Context(), recipe.ID)
	require.NoError(t, err)
	require.Equal(t, RecipeActive, stored.State)
}

func TestReportedOverageCreatesPersistentAccountingGate(t *testing.T) {
	for _, dimension := range []string{"tokens", "cost"} {
		t.Run(dimension, func(t *testing.T) {
			limits := DefaultLimits()
			if dimension == "tokens" {
				limits.MaxTokens = 1500
			} else {
				limits.MaxCostMicroUSD = 2000
			}
			session, budget := budgetFixture(limits)
			require.NoError(t, budget.BeforeRequest(t.Context(), requestFixture("first", 1)))
			usage := usageFixture("first", 1)
			usage.Tokens.InputTokens = 2000
			err := budget.ObserveUsage(t.Context(), usage)
			require.Equal(t, FailureBudget, ErrorCode(err))
			require.NotNil(t, session.accountingErr)
			require.ErrorIs(t, budget.BeforeRequest(t.Context(), requestFixture("next", 1)), session.accountingErr)
			require.Equal(t, 1, session.Metrics().HTTPAttempts)
		})
	}
}

func TestReservationOverflowBlocksRequestsExplicitly(t *testing.T) {
	for _, dimension := range []string{"tokens", "cost"} {
		t.Run(dimension, func(t *testing.T) {
			session, budget := budgetFixture(DefaultLimits())
			if dimension == "tokens" {
				session.reserved["a"] = reservation{tokens: math.MaxInt64}
				session.reserved["b"] = reservation{tokens: 1}
			} else {
				session.reserved["a"] = reservation{cost: math.MaxInt64}
				session.reserved["b"] = reservation{cost: 1}
			}
			err := budget.BeforeRequest(t.Context(), requestFixture("next", 1))
			require.Equal(t, FailureBudget, ErrorCode(err))
			require.NotNil(t, session.accountingErr)
			require.Zero(t, session.Metrics().HTTPAttempts)
		})
	}
}

func TestRecipeSaveReadFailureRollsBackPromotion(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	_, _, _, _, recipe := recipeFixture(t)
	_, err := store.DB.Exec("CREATE TRIGGER corrupt_recipe_read AFTER INSERT ON job_page_recipes BEGIN UPDATE job_page_recipes SET definition_hash='corrupt' WHERE id=NEW.id; END")
	require.NoError(t, err)
	_, err = store.SaveVerified(t.Context(), recipe)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	var count int
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM job_page_recipes").Scan(&count))
	require.Zero(t, count)
	_, err = store.GetRecipe(t.Context(), recipe.ID)
	require.ErrorIs(t, err, ErrRecipeNotFound)
	require.Empty(t, ErrorCode(err))
}
