# MiniKV — Architecture

MiniKV is a single-binary, single-node persistent key-value store. This
document describes how the pieces fit; the on-disk formats are specified in
`docs/design-decisions.md` and the recovery procedure in `docs/recovery.md`.

## Big picture

```text
            ┌────────────┐
  HTTP ───► │  api       │───► engine.Engine (interface)
  CLI ────► │  cli       │        │
            └────────────┘        ▼
                           ┌─────────────────┐
                           │ engine          │
                           │  DurableStore   │
                           │   ├─ Store      │  sharded in-memory state
                           │   └─ WAL        │  durability
                           └───────┬─────────┘
                                   ▼
                           ┌─────────────────┐
                           │  persistence    │  WAL frames, snapshots,
                           └─────────────────┘  recovery
```

Dependency rule: **api and cli depend on the engine interface; engine
depends on persistence; persistence depends only on kverrors.** Nothing in
persistence knows HTTP or the CLI exist, and transports never touch files.

## Package map

| Package | Responsibility | Key types |
| --- | --- | --- |
| `cmd/minikv` | argument dispatch, exit codes | `run` |
| `internal/api` | REST transport, JSON validation, error mapping | `Server`, handlers |
| `internal/cli` | server lifecycle + client subcommands | `RunServer`, `RunGet`, ... |
| `internal/engine` | storage contract, sharded store, TTL, durability | `Engine`, `Store`, `DurableStore` |
| `internal/persistence` | WAL framing, snapshots, recovery | `WAL`, `WriteSnapshot`, `Recover` |
| `internal/kverrors` | typed errors shared by all layers | `Error`, `Kind`, `IsKind` |
| `internal/config` | defaults + validation | `Config` |
| `internal/logging` | slog JSON logging | `New`, `ParseLevel` |
| `tests/` | end-to-end integration tests | — |

## The Engine contract

`engine.Engine` is the boundary every transport consumes:

```go
Get(key) ([]byte, error)
Set(key, value, expiresAt) (existed bool, err error)
Delete(key) (existed bool, err error)
Keys() ([]string, error)
Snapshot() (map[string][]byte, error)
SnapshotWithExpiry() (map[string][]byte, map[string]time.Time, error)
Stats() Stats
Close() error
```

Two implementations exist:

- **`Store`** — pure in-memory, sharded, TTL janitor. Used directly in
  unit tests and embedded scenarios.
- **`DurableStore`** — wraps a `Store` with WAL-first mutation and
  recovery-on-open. This is what `minikv server` runs.

## Sharded store internals

- The keyspace is split into a power-of-two number of shards (default 32),
  routed by FNV-1a hash of the key, masked by the shard count.
- Each shard owns one `sync.RWMutex` and one map. **No code path ever
  holds two shard locks at once**; multi-shard operations (`Keys`,
  `Snapshot`, janitor sweeps) take one shard at a time. Cross-shard
  consistency is therefore not linearizable — an accepted, documented
  trade-off for a single-node educational store.
- Global statistics use atomic counters; there is no global lock.
- Values are copied on `Set`; `Get` returns an internal view (read-only by
  contract); `Snapshot` deep-copies so persistence can write files without
  holding locks.
- TTL: absolute expiration timestamps. Enforced lazily on access, and
  eagerly by a janitor goroutine that purges shard by shard.

## Durability path (write)

```text
Set(key, value, ttl)
  1. validate key/value/ttl
  2. compute absolute expiry deadline
  3. WAL.Append(frame)          ← flush (+ fsync under durability=always)
  4. apply to Store             ← only after the append succeeded
  5. respond
```

If step 3 fails, step 4 never happens and the caller sees the error: the
in-memory state never runs ahead of the log. If step 3 succeeds but step 4
fails (a bug, not an expected path), the error says recovery will restore
the key on restart — the log is the source of truth.

The expiry deadline is recomputed after the append so a slow fsync can
never produce a record that is dead-on-apply (a divergence class the
Phase 5 stress tests caught and fixed).

## Recovery path (open)

```text
Open(dir)
  1. Load latest valid snapshot (if any)
  2. Replay every WAL record in order (SET and DELETE)
  3. Skip (and count) records already expired
  4. Report statistics
```

The first invalid WAL frame aborts recovery with a `wal_corruption` error
naming the file and byte offset. Recovery never modifies any file. See
`docs/recovery.md` for operator guidance.

## Error model

Every error is `*kverrors.Error` with a machine-readable kind, the failing
operation, an explanation, an optional cause, and a hint. The HTTP layer
maps kinds to status codes (404 key_not_found, 400 invalid_*, 500
wal_corruption, ...) and always emits the same JSON shape.

## Concurrency rules

- One shard lock at a time, never nested.
- No file I/O while holding a shard lock.
- TTL cleanup shares the same locks as reads/writes, so it can never race
  with them (it *is* the same critical sections).
- The race detector is part of CI (linux/windows/macOS); stress tests in
  `internal/engine/durable_test.go` are its primary targets.
