package applicationanswers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/domain"
)

func TestThousandsOfLearnedMappingsStayOutsideBoundedCandidateInbox(t *testing.T) {
	svc, cfg, database := setup(t)
	ctx := context.Background()
	p := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{NoticePeriod: "Two weeks", FormAnswers: map[string]string{"legacy": "Keep this supplied fact"}}}
	require.NoError(t, cfg.Set("candidate", "resume_profile", p))
	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO form_field_mappings(id,field_sig,field_label,field_type,source,action,user_id,platform) VALUES(?,?,'Learned wording','text','llm','fill','candidate','seek')`)
	require.NoError(t, err)
	for i := 0; i < 2000; i++ {
		id := fmt.Sprintf("learned-%04d", i)
		_, err = stmt.ExecContext(ctx, id, id)
		require.NoError(t, err)
	}
	require.NoError(t, stmt.Close())
	require.NoError(t, tx.Commit())
	view, err := svc.Get(ctx, "candidate")
	require.NoError(t, err)
	require.Len(t, view.Catalog, 10)
	require.Equal(t, p.ApplicationDefaults, view.ApplicationDefaults)
	empty, err := svc.ListReviews(ctx, "candidate", "", "active", 20)
	require.NoError(t, err)
	require.Empty(t, empty.Items, "learned cache variants never create candidate questions")
	for i := 0; i < 55; i++ {
		id := fmt.Sprintf("required-%04d", i)
		_, err = svc.RecordEncounter(ctx, "candidate", domain.ApplicationAnswerEncounter{ID: id, FieldSignature: id, Question: "Describe the condition for this application", ContextKey: id, Employer: "Fixture", Platform: "seek", Status: "required", ProfileRevision: applicationanswers.ProfileRevision(&p)})
		require.NoError(t, err)
	}
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := svc.ListReviews(ctx, "candidate", cursor, "active", 20)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Items), 20)
		require.Equal(t, 55, page.UnresolvedCount)
		for _, item := range page.Items {
			require.False(t, seen[item.ID], "deterministic cursor pages must not duplicate reviews")
			seen[item.ID] = true
		}
		pages++
		if page.NextCursor == "" {
			break
		}
		require.Less(t, pages, 4)
		cursor = page.NextCursor
	}
	require.Len(t, seen, 55)
	require.Equal(t, 3, pages)
	var mappings int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM form_field_mappings WHERE user_id='candidate'`).Scan(&mappings))
	require.Equal(t, 2000, mappings, "review pagination preserves all learned mappings")
}
