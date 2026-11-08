// Package version holds build metadata injected via -ldflags.
package version

var (
	// Version is the release version, e.g. v0.1.0.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// BuildDate is the UTC build timestamp.
	BuildDate = "unknown"
)
