package persistence

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"time"

	"minikv/internal/kverrors"
)

// RecoveryStats reports what recovery did, for logs and /v1/status.
type RecoveryStats struct {
	// SnapshotUsed reports whether a snapshot was loaded.
	SnapshotUsed bool
	// SnapshotEntries is the number of entries restored from the snapshot.
	SnapshotEntries int
	// WALRecords is the number of WAL entries replayed.
	WALRecords int
	// AppliedRecords is the number of WAL entries that changed state.
	AppliedRecords int
	// SkippedExpired is the number of replayed SET entries already expired.
	SkippedExpired int
	// WALBytes is the WAL file size in bytes.
	WALBytes int64
	// Duration is the total recovery time.
	Duration time.Duration
}

// Applier receives replayed WAL entries. Implemented by the persistence
// orchestrator in the engine wrapper; Decode output goes straight here.
type Applier interface {
	// ApplyWAL applies one replayed entry. It must treat expired SET
	// entries as no-ops and return whether it changed state.
	ApplyWAL(e Entry) (applied bool, err error)
}

// Recover restores state by:
//
//  1. Loading the latest valid snapshot (if any).
//  2. Replaying every valid WAL record in order after it.
//  3. Reporting statistics; TTL enforcement happens in the store, which
//     treats expired entries as missing.
//
// Corruption policy: the first invalid WAL frame stops replay and returns a
// WALCorruption error carrying the file path and byte offset. The WAL file
// is NEVER truncated, rewritten, or deleted — the operator decides what to
// do (see docs/recovery.md). The caller may inspect the valid prefix via
// ValidWALBytes when recovery reports corruption.
func Recover(dir string, apply Applier, now time.Time) (RecoveryStats, error) {
	const op = "recovery.Recover"
	start := time.Now()

	stats := RecoveryStats{}

	// Step 1: latest valid snapshot.
	snapData, _, ok, err := LoadSnapshot(dir)
	if err != nil {
		return stats, err // already typed, path included
	}
	if ok {
		stats.SnapshotUsed = true
		stats.SnapshotEntries = len(snapData)
		for k, v := range snapData {
			if _, err := apply.ApplyWAL(Entry{Op: OpSet, Key: []byte(k), Value: v}); err != nil {
				return stats, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
					"could not restore snapshot entry into the store: recovery aborted, no data was modified on disk")
			}
			stats.AppliedRecords++
		}
	}

	// Step 2: replay the WAL from the beginning. Snapshot and WAL semantics:
	// the snapshot is a base; every WAL record after it (SET and DELETE) is
	// applied in order, so deletes recorded after the snapshot win.
	walPath := filepath.Join(dir, WALFileName)
	f, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			stats.Duration = time.Since(start)
			return stats, nil
		}
		return stats, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
			"could not open the WAL file for replay: check permissions on the data directory")
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return stats, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
			"could not stat the WAL file")
	}
	stats.WALBytes = info.Size()

	reader := bufio.NewReader(f)
	offset := int64(0)
	for {
		entry, next, err := Decode(reader, offset)
		if err == io.EOF {
			break // clean end of file
		}
		if err != nil {
			// Corruption: report file and offset; never truncate or delete.
			return stats, kverrors.Wrap(kverrors.WALCorruption, op, err,
				"WAL replay stopped at %s offset %d: the file was NOT modified; see docs/recovery.md for the documented repair process", walPath, offset)
		}
		stats.WALRecords++
		applied, err := apply.ApplyWAL(entry)
		if err != nil {
			return stats, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
				"WAL replay failed at %s offset %d: recovery aborted, the file was NOT modified", walPath, offset)
		}
		if applied {
			stats.AppliedRecords++
		} else {
			stats.SkippedExpired++
		}
		offset = next
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

// ValidWALBytes returns the length of the valid prefix of the WAL file: the
// number of bytes from the start that decode cleanly. After a corruption
// report this tells the operator how much of the log is intact. It never
// modifies the file.
func ValidWALBytes(dir string) (int64, error) {
	f, err := os.Open(filepath.Join(dir, WALFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, kverrors.Wrap(kverrors.DataDirFailure, "wal.ValidPrefix", err,
			"could not open the WAL file")
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var offset int64
	for {
		_, next, err := Decode(reader, offset)
		if err == io.EOF {
			return next, nil // next == file size here
		}
		if err != nil {
			return offset, nil // valid prefix length before the bad frame
		}
		offset = next
	}
}
