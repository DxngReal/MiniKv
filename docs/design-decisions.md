# MiniKV — Design Decisions

This document explains why MiniKV looks the way it does and records the
exact on-disk formats. The specification lives in
`docs/MASTER_BUILD_PROMPT.md`.

## 1. Language and dependencies

Go with the standard library only. MiniKV is an educational systems
project: every dependency would hide a concept the project is meant to
teach (concurrency, file I/O, framing). No Redis code or external
key-value implementation was consulted or copied.

## 2. Package boundaries

```text
internal/engine        storage contract + in-memory sharded store
internal/persistence   WAL, snapshots, recovery (no HTTP/CLI imports)
internal/api           HTTP transport
internal/cli           CLI transport
internal/kverrors      typed errors shared by all layers
```

The engine depends on persistence (to build a durable store) but never on
api or cli. Persistence depends only on kverrors. Transport layers
depend on the engine interface, not on concrete types, so tests can swap
implementations.

## 3. Sharded locking in the store

A single global mutex is simple but serializes all traffic; a lock-free
design is far too complex for the scope. MiniKV shards the keyspace into
a power-of-two number of shards (default 32), each with its own
`sync.RWMutex`. Key routing uses FNV-1a, masked by the shard count.

Locking rules (also documented in `internal/engine/store.go`):

- Code must never hold two shard locks at the same time. Multi-shard
  operations (`Keys`, `Snapshot`, `Stats`, janitor sweeps) lock one shard
  at a time, release it, and move on. Consistency across shards is
  therefore not linearizable; that is acceptable for a single-node,
  educational store and is documented honestly.
- Global statistics use atomic counters, so no cross-shard lock exists.
- Slow operations (file I/O) never happen while holding a shard lock:
  `Snapshot()` copies data out under brief per-shard read locks, then the
  persistence layer writes the file.

## 4. TTL model

Entries carry an absolute expiration timestamp. Expired entries behave
exactly like missing ones, enforced in three places:

- lazily, on `Get` (dead entry removed, `KeyNotFound` returned),
- by a background janitor goroutine that periodically purges dead
  entries shard by shard,
- at recovery, where expired WAL records are skipped and reported.

Absolute timestamps (not relative TTLs) are stored so surviving records
mean the same thing after a restart.

## 5. Write-ahead logging

Every mutation is appended to the WAL before it is acknowledged. The
default durability mode fsyncs every append, so a completed operation
survives an abrupt process termination. `DurabilityNever` (buffered,
no fsync) exists for tests and experiments only and must never become
the default.

### 5.1 WAL frame layout (implementation source of truth)

All integers are little-endian. The file is a pure sequence of frames;
there is no file-level header.

```text
offset  size  field
0       4     magic: 0x4B4D564B ("KVMK")
4       1     format version (currently 1)
5       1     operation type: 1 = SET, 2 = DELETE
6       8     key length K (uint64)
14      8     value length V (uint64; 0 for DELETE)
22      8     expiration, unix nanoseconds (0 = no expiration)
30      K     key bytes
30+K    V     value bytes (absent for DELETE)
30+K+V  4    CRC32-Castagnoli of bytes [4, 30+K+V)
```

Reasons:

- **Magic + version** let recovery reject files that are not a MiniKV
  WAL and give room to evolve the format.
- **Length-prefixed key/value** make framing trivial and corrupted
  headers detectable (lengths are bounded by 4 MiB so a corrupt length
  can never cause a huge allocation).
- **CRC over everything after the magic** catches corruption in the
  version byte, op, lengths, timestamp, and payload.
- **CRC32-Castagnoli** (hardware-accelerated on modern CPUs) over SHA-x:
  we need error detection, not cryptography.

### 5.2 Corruption policy

Replay stops at the first invalid frame (bad magic, unknown version or
op, absurd lengths, truncation, checksum mismatch) and returns a
`WALCorruption` error naming the file and the byte offset. The WAL file
is **never** truncated, rewritten, or deleted by MiniKV. The operator
inspects `ValidWALBytes` (the length of the intact prefix) and decides
what to do; see `docs/recovery.md`. A torn final record (crash during
append) surfaces as the same reported corruption.

### 5.3 WAL growth

v0.1.0 never compacts the WAL; it grows until the operator deletes or
truncates it manually after taking a snapshot (documented in
`docs/recovery.md`). Automatic compaction is a roadmap item. This is an
honest trade-off: compaction adds significant complexity and is not
needed to teach WAL and recovery concepts.

## 6. Snapshots

### 6.1 Snapshot file layout

All integers little-endian.

```text
offset  size  field
0       4     magic: 0x534D564B ("KVMS")
4       1     format version (currently 1)
5       8     entry count N (uint64)
13      8     created-at, unix seconds
21      ...   N entries, each:
                8  key length K
                8  value length V
                8  expiration, unix nanoseconds (0 = none)
                K  key bytes
                V  value bytes
end     4    CRC32-Castagnoli of bytes [4, end)
```

Keys are stored sorted so files are byte-identical for identical
content, which makes diffing and testing deterministic.

### 6.2 Atomic replacement

Snapshots are written to `snapshot.bin.tmp` in the same directory,
flushed, fsynced, closed, then renamed over `snapshot.bin`. A directory
sync is attempted afterwards where the platform supports it (POSIX). If
any step fails, the temporary file is removed and the previous snapshot
is untouched — a half-written snapshot can never become the latest one.

## 7. Recovery order

1. Load the latest valid snapshot, if any.
2. Replay every WAL record in order (SET and DELETE), so deletes
   recorded after the snapshot win.
3. Skip and count expired SET records.
4. Report statistics (snapshot used, records replayed/applied, expired,
   WAL size, duration).

Any corruption aborts recovery with a typed error; nothing on disk is
modified during recovery.

## 8. Errors as values

All errors are `*kverrors.Error` with a machine-readable kind, the
failing operation, a message, an optional cause, and a hint. Callers
match on kinds (`kverrors.IsKind`) instead of string comparison. Errors
explain what happened, why, and what to do next, and never contain key
or value contents.

## 9. Byte values, UTF-8 at the edges

The engine stores `[]byte`. The HTTP and CLI layers may expose UTF-8
strings. This keeps the core honest about what it stores (bytes) while
keeping transports friendly.

## 10. What MiniKV deliberately does not do

- No replication, clustering, or consensus (single node by design).
- No authentication or TLS; bind to loopback if exposure matters.
- No range queries or transactions.
- No automatic WAL compaction in v0.1.0.
