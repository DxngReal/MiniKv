// Package kverrors defines the typed error model shared by all MiniKV
// packages: engine, persistence, api, and cli.
//
// Every MiniKV error carries:
//
//   - Kind: a machine-readable classification so callers can react
//     programmatically instead of matching on strings,
//   - Op: the operation that failed, for example "engine.Set" or "wal.Append",
//   - Message: what happened and why, without key or value contents,
//   - Err: the optional wrapped underlying cause,
//   - a hint describing what the caller can do next.
//
// Errors never embed key or value contents, secrets, or credentials.
package kverrors

import (
	"errors"
	"fmt"
	"strings"
)

// Kind classifies a MiniKV error. Always use one of the constants below.
type Kind string

// Error kinds used across MiniKV. The names follow the typed errors required
// by the project specification (PROJECT_CONTEXT.md).
const (
	// KeyNotFound means the requested key does not exist or has expired.
	KeyNotFound Kind = "key_not_found"
	// InvalidKey means the key is empty, too long, or otherwise not usable.
	InvalidKey Kind = "invalid_key"
	// InvalidValue means the value is nil or larger than the configured limit.
	InvalidValue Kind = "invalid_value"
	// InvalidTTL means the requested TTL is negative or not usable.
	InvalidTTL Kind = "invalid_ttl"
	// WALCorruption means a write-ahead-log record failed checksum or framing
	// validation. Corruption is reported, never silently removed.
	WALCorruption Kind = "wal_corruption"
	// SnapshotFailure means a snapshot could not be created or written.
	SnapshotFailure Kind = "snapshot_failure"
	// RecoveryFailure means state could not be restored from disk.
	RecoveryFailure Kind = "recovery_failure"
	// DataDirFailure means the data directory could not be created or used.
	DataDirFailure Kind = "data_dir_failure"
	// ServerFailure means the HTTP server failed to start or serve.
	ServerFailure Kind = "server_failure"
	// StoreClosed means an operation was attempted after Close.
	StoreClosed Kind = "store_closed"
)

// Hint returns actionable advice for a kind: what the caller can do next.
func (k Kind) Hint() string {
	switch k {
	case KeyNotFound:
		return "check the key name; create it with SET before reading it"
	case InvalidKey:
		return "use a non-empty key of at most 512 bytes"
	case InvalidValue:
		return "store a value of at most 1 MiB"
	case InvalidTTL:
		return "use a positive TTL, or omit it for no expiration"
	case WALCorruption:
		return "preserve the WAL file; see docs/recovery.md for the documented repair process"
	case SnapshotFailure:
		return "check disk space and permissions on the data directory, then retry"
	case RecoveryFailure:
		return "check the data directory; recovery details are in docs/recovery.md"
	case DataDirFailure:
		return "check that the data directory path exists and is writable"
	case ServerFailure:
		return "check the listen address and logs, then restart"
	case StoreClosed:
		return "recreate the store before issuing operations"
	default:
		return "inspect the error message and logs for details"
	}
}

// Error is the concrete error type produced by MiniKV operations.
type Error struct {
	// Kind classifies the failure.
	Kind Kind
	// Op names the failing operation, for example "engine.Set".
	Op string
	// Message explains what happened and why, without key or value contents.
	Message string
	// Err is the optional wrapped underlying cause.
	Err error
}

// Error implements the error interface. The message always ends with a hint
// describing what the caller can do next.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("minikv: ")
	b.WriteString(e.Op)
	b.WriteString(": ")
	b.WriteString(string(e.Kind))
	b.WriteString(": ")
	b.WriteString(e.Message)
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	b.WriteString(". Next step: ")
	b.WriteString(e.Kind.Hint())
	return b.String()
}

// Unwrap returns the wrapped cause so errors.Is and errors.As work.
func (e *Error) Unwrap() error {
	return e.Err
}

// New builds a standalone MiniKV error.
func New(kind Kind, op string, format string, args ...any) *Error {
	return &Error{
		Kind:    kind,
		Op:      op,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap builds a MiniKV error that wraps an underlying cause.
func Wrap(kind Kind, op string, err error, format string, args ...any) *Error {
	return &Error{
		Kind:    kind,
		Op:      op,
		Message: fmt.Sprintf(format, args...),
		Err:     err,
	}
}

// IsKind reports whether err is a MiniKV error of the given kind, unwrapping
// the error chain as needed.
func IsKind(err error, kind Kind) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == kind
	}
	return false
}

// KindOf returns the kind of err if it is a MiniKV error, unwrapping the
// error chain as needed.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return "", false
}
