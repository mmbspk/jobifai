package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStampedBuildIdentityWins(t *testing.T) {
	stamped := Metadata{Version: "v1.2.3", GitSHA: "1234567890123456789012345678901234567890", BuildTime: "2026-11-26T01:00:00Z", SourceState: "clean"}
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "different-checkout"}, {Key: "vcs.modified", Value: "true"}}}
	require.Equal(t, stamped, resolveBuildInfo(stamped, info))
}

func TestPlainBuildUsesCompiledRevisionAndMarksChanges(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1234567890123456789012345678901234567890"},
		{Key: "vcs.modified", Value: "true"},
		{Key: "vcs.time", Value: "2026-11-20T01:00:00Z"},
	}}
	got := resolveBuildInfo(Metadata{Version: "dev", GitSHA: "unknown", SourceState: "unknown"}, info)
	require.Equal(t, "123456789012-dirty", got.Version)
	require.Equal(t, info.Settings[0].Value, got.GitSHA)
	require.Equal(t, "modified", got.SourceState)
	require.Empty(t, got.BuildTime, "a commit date must not be reported as build time")
}

func TestUnversionedBuildStaysExplicitlyUnknown(t *testing.T) {
	stamped := Metadata{Version: "dev", GitSHA: "unknown", SourceState: "unknown"}
	require.Equal(t, stamped, resolveBuildInfo(stamped, nil))
}

func TestDirtySuffixIsAddedOnlyOnce(t *testing.T) {
	stamped := Metadata{Version: "v1.2.3-dirty", SourceState: "modified"}
	require.Equal(t, stamped, resolveBuildInfo(stamped, nil))
}
