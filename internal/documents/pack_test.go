package documents

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationDocumentPack_ReadyForSubmitRequiresPrepared(t *testing.T) {
	t.Parallel()
	p := ApplicationDocumentPack{
		Resume: domain.ApplicationDocumentRef{Outcome: domain.DocumentOutcomeLocalFile},
	}
	require.False(t, p.ReadyForSubmit())
	p.Prepared = true
	require.True(t, p.ReadyForSubmit())
}

func TestApplicationDocumentPack_MarkPreparedClearsDiscoveryHold(t *testing.T) {
	t.Parallel()
	p := ApplicationDocumentPack{HoldReason: "Use Prepare documents to scan the apply form"}
	p.MarkPrepared()
	require.True(t, p.Prepared)
	require.Empty(t, p.HoldReason)
}
