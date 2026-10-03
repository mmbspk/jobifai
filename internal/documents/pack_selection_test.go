package documents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseApplicationPackJSON_PreservesSelectionAfterPickerChange(t *testing.T) {
	t.Parallel()
	sel := ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true}
	raw := WriteApplicationPackJSON(PackAfterSelectionChange(sel))
	require.NotEmpty(t, raw)
	got := ParseApplicationPackJSON(raw)
	require.True(t, got.Selection.ResumeUseSite)
	require.True(t, got.Selection.CoverSkip)
	require.Contains(t, got.HoldReason, "prepare again")
}

func TestBuildReviewDocumentOptions_EmptyListsJSONArrays(t *testing.T) {
	t.Parallel()
	opts := BuildReviewDocumentOptions(ListResponse{})
	b, err := json.Marshal(opts)
	require.NoError(t, err)
	require.Contains(t, string(b), `"resume":[]`)
	require.Contains(t, string(b), `"cover":[]`)
}
