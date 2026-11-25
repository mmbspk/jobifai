package jobpage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func recipeFixture(t *testing.T) (Snapshot, Plan, Result, Verification, Recipe) {
	t.Helper()
	s := fixtureSnapshot()
	p := Plan{Version: SnapshotVersion, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}}
	live := Result{Target: s.Target, Identity: s.Identity, Description: s.Nodes[1].Text, Source: SourceModel, LayoutSignature: s.LayoutSignature}
	v := Verification{SnapshotID: s.ID, Verdicts: []Verdict{{Ref: "n1", IsDescription: true, Supported: true, Complete: true}}}
	r, err := BuildVerifiedRecipe(context.Background(), s, p, live, v, DefaultLimits())
	require.NoError(t, err)
	return s, p, live, v, r
}

func TestRecipeUsesCurrentJobStructureAndFreshRefs(t *testing.T) {
	_, _, _, _, r := recipeFixture(t)
	s := fixtureSnapshot()
	s.ID = "new-epoch"
	s.Target, _, _ = NormalizeTarget(Target{URL: "https://www.linkedin.com/jobs/view/456", Title: "Designer", Company: "Different"})
	s.Identity.JobID = "456"
	s.Identity.Title = "Designer"
	s.Identity.Company = "Different"
	s.Identity.HeaderRefs = []string{"n8"}
	s.Nodes[0].Ref = "n8"
	s.Nodes[0].OwnerJobID = "456"
	s.Nodes[0].Text = "Designer"
	s.Nodes[1].Ref = "n9"
	s.Nodes[1].OwnerJobID = "456"
	s.Nodes[1].Text = strings.Repeat("A new current job description. ", 10)
	s.Nodes[1].Attributes["data-job-id"] = "456"
	s.LayoutSignature = StructuralSignature(s)
	p, err := PlanFromRecipe(s, r, DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, []string{"n9"}, p.DescriptionRefs)
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	for _, private := range []string{`"123"`, `"456"`, "Engineer", "Fixture", "private-candidate", "supported current job description", `"n1"`} {
		require.NotContains(t, string(raw), private)
	}
	require.Equal(t, "job_id", r.DescriptionSelectors[0].SubjectAttributes["data-job-id"])
}

func TestRecipeRejectsAmbiguityDriftAndIncompleteReplay(t *testing.T) {
	s, _, _, _, r := recipeFixture(t)
	duplicate := s.Nodes[1]
	duplicate.Ref = "n2"
	s.Nodes = append(s.Nodes, duplicate)
	s.LayoutSignature = StructuralSignature(s)
	_, err := PlanFromRecipe(s, r, DefaultLimits())
	require.Equal(t, FailureAmbiguous, ErrorCode(err))
	s, _, _, _, r = recipeFixture(t)
	s.Nodes[1].Attributes["data-testid"] = "changed-layout"
	s.LayoutSignature = StructuralSignature(s)
	_, err = PlanFromRecipe(s, r, DefaultLimits())
	require.Equal(t, FailureStale, ErrorCode(err))
	s, _, _, _, r = recipeFixture(t)
	s.Nodes[1].Collapsed = true
	_, err = PlanFromRecipe(s, r, DefaultLimits())
	require.Equal(t, FailureThin, ErrorCode(err))
	s, _, _, _, r = recipeFixture(t)
	s.Scope.Origin = "https://www.seek.com.au"
	_, err = PlanFromRecipe(s, r, DefaultLimits())
	require.Error(t, err)
}

func TestRecipeOnlyPromotesCompleteIndependentLiveEvidence(t *testing.T) {
	for _, change := range []func(*Snapshot, *Plan, *Result, *Verification){
		func(s *Snapshot, _ *Plan, _ *Result, _ *Verification) { s.Loading = true },
		func(s *Snapshot, _ *Plan, _ *Result, _ *Verification) { s.Truncated = true },
		func(s *Snapshot, _ *Plan, _ *Result, _ *Verification) { s.Nodes[1].Truncated = true },
		func(s *Snapshot, _ *Plan, _ *Result, _ *Verification) { s.Nodes[1].Collapsed = true },
		func(s *Snapshot, _ *Plan, _ *Result, _ *Verification) { s.Nodes[1].TextLength = 9999 },
		func(_ *Snapshot, p *Plan, _ *Result, _ *Verification) {
			p.Actions = []Action{{Kind: ActionWait, Amount: 10}}
		},
		func(_ *Snapshot, _ *Plan, r *Result, _ *Verification) { r.Identity.Verified = false },
		func(_ *Snapshot, _ *Plan, r *Result, _ *Verification) { r.Target.JobID = "999" },
		func(_ *Snapshot, _ *Plan, r *Result, _ *Verification) { r.Description = "Invented replacement content" },
		func(_ *Snapshot, _ *Plan, _ *Result, v *Verification) { v.Verdicts[0].Complete = false },
	} {
		s, p, live, v, _ := recipeFixture(t)
		change(&s, &p, &live, &v)
		_, err := BuildVerifiedRecipe(context.Background(), s, p, live, v, DefaultLimits())
		require.Error(t, err)
	}
	s, p, live, v, _ := recipeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BuildVerifiedRecipe(ctx, s, p, live, v, DefaultLimits())
	require.ErrorIs(t, err, context.Canceled)
}

func TestRecipeDoesNotLearnLiteralOrExecutableSelectors(t *testing.T) {
	s, p, live, v, _ := recipeFixture(t)
	s.Nodes[1].Attributes["data-testid"] = "secret-candidate-record"
	s.Target.RecordID = "secret-candidate-record"
	live.Target = s.Target
	s.Nodes[1].Attributes["aria-label"] = "Engineer at Fixture"
	s.LayoutSignature = StructuralSignature(s)
	live.LayoutSignature = s.LayoutSignature
	r, err := BuildVerifiedRecipe(context.Background(), s, p, live, v, DefaultLimits())
	require.NoError(t, err)
	require.Empty(t, r.DescriptionSelectors[0].Attributes)
	for _, attributes := range []map[string]string{{"class": "generated-abc12345"}, {"data-testid": "123456789"}, {"data-testid": "https://private.example"}, {"aria-label": "Candidate email user@example.com"}} {
		r.DescriptionSelectors[0].Attributes = attributes
		r.ID = recipeID(r)
		_, err := PlanFromRecipe(s, r, DefaultLimits())
		require.Error(t, err)
	}
}
