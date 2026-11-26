package version

import (
	"runtime/debug"
	"strings"
)

// Stamped by scripts/build.sh for native and Docker builds.
var (
	Version     = "dev"
	GitSHA      = "unknown"
	BuildTime   = ""
	SourceState = "unknown"
)

type Metadata struct {
	Version     string `json:"version"`
	GitSHA      string `json:"git_sha"`
	BuildTime   string `json:"build_time"`
	SourceState string `json:"source_state"`
}

func Current() Metadata {
	return Metadata{Version: Version, GitSHA: GitSHA, BuildTime: BuildTime, SourceState: SourceState}
}

func init() {
	info, _ := debug.ReadBuildInfo()
	resolved := resolveBuildInfo(Current(), info)
	Version, GitSHA, BuildTime, SourceState = resolved.Version, resolved.GitSHA, resolved.BuildTime, resolved.SourceState
}

// A plain go build can still identify its compiled revision. Never inspect the
// working checkout at runtime: it may have changed since this binary was built.
func resolveBuildInfo(stamped Metadata, info *debug.BuildInfo) Metadata {
	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if stamped.GitSHA == "unknown" {
					stamped.GitSHA = setting.Value
				}
			case "vcs.modified":
				if stamped.SourceState == "unknown" {
					switch setting.Value {
					case "true":
						stamped.SourceState = "modified"
					case "false":
						stamped.SourceState = "clean"
					}
				}
			}
		}
	}
	if stamped.Version == "dev" && stamped.GitSHA != "unknown" && len(stamped.GitSHA) >= 12 {
		stamped.Version = stamped.GitSHA[:12]
	}
	if stamped.SourceState == "modified" && !strings.HasSuffix(stamped.Version, "-dirty") {
		stamped.Version += "-dirty"
	}
	// vcs.time is a commit date, not the build timestamp. Leave an unstamped
	// build time empty rather than misrepresenting when the artifact was made.
	return stamped
}
