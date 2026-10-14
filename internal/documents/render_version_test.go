package documents

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/resume"
)

func TestRenderSnapshot_SelectsStoredRendererVersion(t *testing.T) {
	newSnapshot, err := BuildRenderSnapshot("", "")
	require.NoError(t, err)
	require.Equal(t, resume.PDFRendererVersion, newSnapshot.RendererVersion)
	legacy, err := ParseRenderSnapshot(`{"renderer_version":"jobifai-pdf-v1","section_labels":{"summary":"Saved summary"}}`)
	require.NoError(t, err)
	require.NoError(t, AssertSupportedRenderer(legacy.RendererVersion))
	options := RenderOptsFromSnapshot(legacy)
	require.Equal(t, resume.LegacyPDFRendererVersion, options.RendererVersion)
	require.Equal(t, "Saved summary", options.SectionLabels.Summary)
	require.Error(t, AssertSupportedRenderer("unknown-renderer"))
}
