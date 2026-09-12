// Package logging provides MiniKV's structured logger on top of log/slog.
//
// Logging rules from the project specification:
//
//   - Log startup, shutdown, configuration summaries, recovery results, and
//     persistence failures at the appropriate level.
//   - Levels: DEBUG, INFO, WARN, ERROR.
//   - Never log values, credentials, tokens, or full request bodies.
//
// Output is one JSON object per line so standard log tooling can parse it.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel converts a level name into a slog.Level. Accepted names,
// case-insensitive with optional surrounding whitespace: "debug", "info",
// "warn" or "warning", "error".
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("logging: unknown level %q: use debug, info, warn, or error", name)
	}
}

// New returns a structured JSON logger that writes to w at the given level.
// The logger never logs key or value contents; callers are responsible for
// passing only safe attributes.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
