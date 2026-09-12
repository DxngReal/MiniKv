// Package persistence provides MiniKV's durable storage: a framed,
// checksummed write-ahead log, atomic snapshots, and crash recovery that
// loads the latest valid snapshot and replays the WAL after it.
//
// Persistence rules from the specification:
//
//   - Every mutation is appended to the WAL before it is acknowledged.
//   - The default durability mode fsyncs each mutation.
//   - WAL records are checksummed with CRC32 (Castagnoli table).
//   - Recovery order: latest valid snapshot, then WAL records after it,
//     then TTL rules, then recovery statistics.
//   - A checksum mismatch, invalid frame, or truncated record produces a
//     recovery error containing the file name and byte offset.
//   - Corrupted WAL data is never silently truncated or deleted.
//
// This package depends only on the engine contract and kverrors; it never
// imports HTTP or CLI code.
package persistence

// File names inside the data directory.
const (
	WALFileName      = "wal.log"
	SnapshotFileName = "snapshot.bin"
	SnapshotTmpName  = "snapshot.bin.tmp"
)

// OpType is the operation recorded in a WAL entry.
type OpType byte

// WAL operation types.
const (
	// OpSet stores a key/value pair with an expiration timestamp.
	OpSet OpType = 1
	// OpDelete removes a key.
	OpDelete OpType = 2
)

// valid reports whether the operation byte is known.
func (op OpType) valid() bool {
	return op == OpSet || op == OpDelete
}

// Durability selects when WAL writes reach the disk.
type Durability string

// Durability modes mirrored from internal/config to keep persistence
// independent of the config package.
const (
	// DurabilityAlways fsyncs after every append. Required as the default.
	DurabilityAlways Durability = "always"
	// DurabilityNever never fsyncs; recent writes may be lost after an
	// abrupt termination. For tests and experiments only.
	DurabilityNever Durability = "never"
)
