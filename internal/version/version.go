package version

// Set at link time via -ldflags; defaults for local dev.
var (
	Version = "dev"
	GitSHA  = "unknown"
)
