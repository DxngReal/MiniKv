package engine

import (
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"minikv/internal/kverrors"
	"minikv/internal/persistence"
)

// DurableStore combines the in-memory sharded store with the WAL and
// snapshot machinery: every mutation is appended to the WAL first and only
// then applied in memory, and Open recovers state from disk.
//
// Locking: DurableStore holds no locks of its own; the WAL and the store
// each serialize their own operations. Close ordering is store-first, then
// WAL, so no mutation can be applied after the log is closed.
//
// Durability rule: if a WAL append fails, the mutation is NOT applied in
// memory and the error is returned to the caller — memory never diverges
// from the log.
type DurableStore struct {
	store *Store
	wal   *persistence.WAL
	dir   string

	walBytes        atomic.Int64
	snapshotEntries atomic.Int64
	durabilityMode  string
}

// compile-time proof that DurableStore implements Engine.
var _ Engine = (*DurableStore)(nil)

// Durability mode reported in Stats, mirroring config values without a
// config dependency.
const (
	// DurabilityAlwaysMode fsyncs every WAL append (the default).
	DurabilityAlwaysMode = "always"
	// DurabilityNeverMode never fsyncs (tests and experiments only).
	DurabilityNeverMode = "never"
)

// Open recovers state in dir (latest snapshot, then WAL replay) and returns
// a durable engine ready to accept mutations. The data directory is created
// if missing.
func Open(dir string, durability persistence.Durability, logger *slog.Logger) (*DurableStore, error) {
	const op = "engine.Open"

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, kverrors.Wrap(kverrors.DataDirFailure, op, err,
			"could not create the data directory: check the path and permissions")
	}

	store, err := New(DefaultOptions())
	if err != nil {
		return nil, kverrors.Wrap(kverrors.ServerFailure, op, err, "could not start the store")
	}

	stats, err := persistence.Recover(dir, recoveryApplier{store}, time.Now())
	if err != nil {
		_ = store.Close()
		return nil, err // already typed with file and offset where relevant
	}
	if logger != nil {
		logger.Info("recovery complete",
			"snapshot_used", stats.SnapshotUsed,
			"snapshot_entries", stats.SnapshotEntries,
			"wal_records", stats.WALRecords,
			"applied_records", stats.AppliedRecords,
			"skipped_expired", stats.SkippedExpired,
			"wal_bytes", stats.WALBytes,
			"duration_ms", stats.Duration.Milliseconds(),
		)
	}

	wal, err := persistence.OpenWAL(dir, durability)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	ds := &DurableStore{
		store: store,
		wal:   wal,
		dir:   dir,
		durabilityMode: func() string {
			if durability == persistence.DurabilityAlways {
				return DurabilityAlwaysMode
			}
			return DurabilityNeverMode
		}(),
	}
	ds.walBytes.Store(stats.WALBytes)
	if stats.SnapshotUsed {
		ds.snapshotEntries.Store(int64(stats.SnapshotEntries))
	} else {
		ds.snapshotEntries.Store(NoSnapshot)
	}
	return ds, nil
}

// recoveryApplier adapts the store to the persistence.Applier interface.
// Snapshot entries and WAL SETs land via the internal set path; DELETEs via
// the internal delete path. Expired WAL SETs are skipped and counted.
type recoveryApplier struct{ s *Store }

func (a recoveryApplier) ApplyWAL(e persistence.Entry) (bool, error) {
	switch e.Op {
	case persistence.OpSet:
		key := string(e.Key)
		if !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(time.Now()) {
			return false, nil // expired before recovery finished
		}
		var exp *time.Time
		if !e.ExpiresAt.IsZero() {
			exp = &e.ExpiresAt
		}
		if err := a.s.setFromRecovery(key, e.Value, exp); err != nil {
			return false, err
		}
		return true, nil
	case persistence.OpDelete:
		_, _ = a.s.Delete(string(e.Key))
		return true, nil
	default:
		return false, kverrors.New(kverrors.RecoveryFailure, "engine.recoveryApplier",
			"unknown operation type %d in WAL record", e.Op)
	}
}

// Dir returns the data directory (used by tests and status tooling).
func (d *DurableStore) Dir() string { return d.dir }

// Get forwards to the in-memory store.
func (d *DurableStore) Get(key string) ([]byte, error) { return d.store.Get(key) }

// Keys forwards to the in-memory store.
func (d *DurableStore) Keys() ([]string, error) { return d.store.Keys() }

// Stats merges store counters with WAL and snapshot sizes.
func (d *DurableStore) Stats() Stats {
	st := d.store.Stats()
	if wb := d.walBytes.Load(); wb >= 0 {
		st.WALBytes = wb
	}
	st.SnapshotEntries = d.snapshotEntries.Load()
	st.DurabilityMode = d.durabilityMode
	return st
}

// Set appends the mutation to the WAL first; only a durable append is
// applied in memory.
func (d *DurableStore) Set(key string, value []byte, expiresAt *time.Time) (bool, error) {
	const op = "engine.Set"

	if err := ValidateKey(key, MaxKeyBytes); err != nil {
		return false, err
	}
	if err := ValidateValue(value, MaxValueBytes); err != nil {
		return false, err
	}
	var expNano time.Time
	if expiresAt != nil {
		if !expiresAt.IsZero() && !expiresAt.After(time.Now()) {
			return false, kverrors.New(kverrors.InvalidTTL, op,
				"expiration time is in the past; expired entries behave as missing ones")
		}
		expNano = *expiresAt
	}

	entry := persistence.Entry{Op: persistence.OpSet, Key: []byte(key), Value: value}
	if !expNano.IsZero() {
		entry.ExpiresAt = expNano
	}
	if err := d.wal.Append(entry); err != nil {
		return false, err // mutation NOT applied; memory stays consistent with the log
	}
	d.walBytes.Store(d.wal.Size())

	existed, err := d.store.Set(key, value, expiresAt)
	if err != nil {
		// The append succeeded, so the record will replay; surface loudly.
		return false, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
			"the WAL append succeeded but the in-memory apply failed; recovery will restore this key on restart")
	}
	return existed, nil
}

// Delete appends the deletion to the WAL first; only a durable append is
// applied in memory.
func (d *DurableStore) Delete(key string) (bool, error) {
	const op = "engine.Delete"

	if err := ValidateKey(key, MaxKeyBytes); err != nil {
		return false, err
	}
	if err := d.wal.Append(persistence.Entry{Op: persistence.OpDelete, Key: []byte(key)}); err != nil {
		return false, err
	}
	d.walBytes.Store(d.wal.Size())

	existed, err := d.store.Delete(key)
	if err != nil {
		return false, kverrors.Wrap(kverrors.RecoveryFailure, op, err,
			"the WAL append succeeded but the in-memory apply failed; recovery will apply this delete on restart")
	}
	return existed, nil
}

// Snapshot (data copy) forwards to the in-memory store, satisfying the
// Engine interface without touching the WAL.
func (d *DurableStore) Snapshot() (map[string][]byte, error) {
	return d.store.Snapshot()
}

// SnapshotStats re-exports the persistence statistics type so transport
// layers can handle snapshot results without importing persistence.
type SnapshotStats = persistence.SnapshotStats

// WriteSnapshot writes an atomic snapshot file of the current state and
// returns statistics. The WAL is not truncated (v0.1.0); see
// docs/recovery.md for the documented WAL handling after a snapshot.
func (d *DurableStore) WriteSnapshot() (SnapshotStats, error) {
	const op = "engine.Snapshot"

	data, expirations, err := d.store.SnapshotWithExpiry()
	if err != nil {
		return persistence.SnapshotStats{}, err
	}
	stats, err := persistence.WriteSnapshot(d.dir, data, expirations, time.Now())
	if err != nil {
		return persistence.SnapshotStats{}, err
	}
	d.snapshotEntries.Store(int64(stats.Entries))
	return stats, nil
}

// SnapshotWithExpiry forwards to the in-memory store.
func (d *DurableStore) SnapshotWithExpiry() (map[string][]byte, map[string]time.Time, error) {
	return d.store.SnapshotWithExpiry()
}

// Close stops the store, then closes the WAL in that order.
func (d *DurableStore) Close() error {
	const op = "engine.Close"

	storeErr := d.store.Close()
	if walErr := d.wal.Close(); walErr != nil {
		return kverrors.Wrap(kverrors.DataDirFailure, op, walErr,
			"could not close the WAL cleanly: run a recovery check before the next start")
	}
	return storeErr
}
