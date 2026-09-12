# MiniKV — Roadmap

Honest scope first: MiniKV is an educational, single-node store. Items
below are learning-oriented extensions, not promises.

## v0.1.0 (this release)

- Sharded in-memory store with TTL
- Checksummed WAL, fsync-per-mutation durability
- Atomic snapshots, crash recovery with explicit corruption reporting
- HTTP REST API and CLI in one binary
- CI on three platforms, Docker image

## Next candidates

- **WAL compaction** — snapshot + truncate the log automatically at a size
  threshold, with a documented, explicit manual path preserved.
- **Group-commit fsync** — batch concurrent appends into one fsync to cut
  the dominant cost of durable writes (~85× vs. buffered on the reference
  machine).
- **Rate-limit / max-keys settings** — simple resource guards for the HTTP
  API.
- **Metrics endpoint** — expose the Stats counters in Prometheus text
  format.

## Deliberately out of scope

- Replication, clustering, consensus, high availability
- Authentication/authorization (keep MiniKV on loopback or behind a proxy)
- Transactions, range queries, secondary indexes
- Redis protocol compatibility
