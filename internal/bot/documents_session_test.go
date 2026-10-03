package bot_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestParseApplicationPack_RestoresSiteAndSkipOutcomes(t *testing.T) {
	t.Parallel()
	pack := documents.PackFromPrepared("", documents.ResolvedDocument{
		Kind: documents.KindResume, Outcome: domain.DocumentOutcomeSiteHosted, UseSite: true,
	}, documents.ResolvedDocument{
		Kind: documents.KindCoverLetter, Outcome: domain.DocumentOutcomeSkipped,
	}, "", "")
	raw := documents.WriteApplicationPackJSON(pack)
	restored := documents.ParseApplicationPackJSON(raw)
	require.Equal(t, domain.DocumentOutcomeSiteHosted, restored.Resume.Outcome)
	require.True(t, restored.Resume.SiteHosted)
	require.Equal(t, domain.DocumentOutcomeSkipped, restored.Cover.Outcome)
}
