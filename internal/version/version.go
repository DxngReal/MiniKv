// Package version holds the single source of truth for the MiniKV version
// reported by the CLI and the /v1/status endpoint.
//
// The version stays at 0.1.0-dev until the v0.1.0 release in Phase 6.
package version

// Version is the semantic version of the MiniKV binary.
const Version = "0.1.0-dev"
