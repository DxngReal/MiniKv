# MiniKV — Roadmap

Honest scope first: MiniKV is an educational, single-node store. Items
below are learning-oriented extensions, not promises.

## v0.1.0 (this release)

- Sharded in-memory store with TTL
- Checksummed WAL, fsync-per-mutation durability
- Atomic snapshots, crash recovery with explicit corruption reporting
- HTTP REST API and CLI in one binary
- CI on three platforms, Docker image

## Delivered since v0.1.0

- **Group-commit fsync** (2026-09-12) — concurrent appends share fsyncs;
  ~11× faster concurrent durable writes on the reference machine, durability
  guarantee unchanged. See `docs/design-decisions.md` §5.0.

## Next candidates

- **WAL compaction** — snapshot + truncate the log automatically at a size
  threshold, with a documented, explicit manual path preserved.
- **Rate-limit / max-keys settings** — simple resource guards for the HTTP
  API.
- **Metrics endpoint** — expose the Stats counters in Prometheus text
  format.

## Deliberately out of scope

- Replication, clustering, consensus, high availability
- Authentication/authorization (keep MiniKV on loopback or behind a proxy)
- Transactions, range queries, secondary indexes
- Redis protocol compatibility
