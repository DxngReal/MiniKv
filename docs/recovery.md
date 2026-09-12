# MiniKV — Recovery Guide

This document is for operators. It explains what MiniKV does after an
abrupt termination, what a corruption report means, and what you can and
cannot do about it.

## Files in the data directory

| File | Purpose | Safe to delete? |
| --- | --- | --- |
| `wal.log` | write-ahead log, source of truth for recent mutations | **No — this is your data** |
| `snapshot.bin` | latest point-in-time image | Only if `wal.log` covers everything since |
| `snapshot.bin.tmp` | temp file of an in-progress snapshot | Yes, if no snapshot is running |

## What happens on start

1. MiniKV loads `snapshot.bin` if a valid one exists.
2. It replays **every** record in `wal.log` in order (both SET and DELETE).
3. Records whose expiration has already passed are skipped and counted.
4. It prints recovery statistics: snapshot used, records replayed and
   applied, expired skipped, WAL size, duration.

A process killed abruptly loses **no acknowledged mutation** in the default
durability mode (`always`): every completed SET/DELETE was fsynced to the
WAL before the client was told it succeeded.

## Corruption reports

If `wal.log` or `snapshot.bin` is damaged (bit rot, partial write, manual
editing, a torn final record from a crash during append), recovery stops at
the first bad frame and reports a typed error like:

```text
minikv: recovery.Recover: wal_corruption: WAL checksum mismatch at offset 612:
recorded 1a2b3c4d, computed 5e6f7081: WAL replay stopped at ./data/wal.log
offset 612: the file was NOT modified; see docs/recovery.md for the
documented repair process. Next step: preserve the WAL file; see
docs/recovery.md for the documented repair process.
```

What MiniKV guarantees:

- The error names the **file** and the **byte offset** of the bad frame.
- **The file is never truncated, rewritten, or deleted** by MiniKV. Repair
  is an explicit operator decision; there is no automatic "fix".

## What the operator can do

1. **Preserve the evidence.** Copy `wal.log` aside before touching anything.
2. **Find the intact prefix** (the number of leading bytes that decode
   cleanly). `ValidWALBytes` in `internal/persistence` reports it, and the
   offset in the error message is exactly the first byte after it.
3. **Choose, explicitly:**
   - *Recover the prefix*: archive the tail from the reported offset onward
     (e.g. `tail -c +613 wal.log > wal.tail`), keep the intact prefix as
     the new `wal.log`, and restart. Everything up to the corruption is
     restored; the tail (usually one torn record) is set aside for manual
     inspection. This is the documented manual repair; MiniKV will not do
     it for you.
   - *Restore from snapshot*: delete `wal.log` **only** if you accept
     losing every mutation recorded after the last snapshot, then restart.
   - *Forensic*: inspect the raw bytes yourself; the frame format is fully
     documented in `docs/design-decisions.md` §5.1.
4. **Restart.** Recovery re-runs from the snapshot and the (repaired) WAL.

A corrupted `snapshot.bin` cannot be worked around by replay alone if the
WAL no longer covers the snapshot era; restore it from backup or start
fresh. Recovery fails loudly rather than silently skipping it.

## Bounding recovery time

Replay cost is linear in WAL size (measured: ~2.2 µs per record on the
reference machine, see `docs/benchmarks.md`). After taking a snapshot you
may archive the old WAL **only while the server is stopped**: stop →
snapshot exists → move `wal.log` aside → restart. v0.1.0 has no automatic
compaction (see `docs/design-decisions.md` §5.3).

## Durability modes

| Mode | fsync | Acknowledged write lost on power cut? |
| --- | --- | --- |
| `always` (default) | per mutation | No |
| `never` | never | Possibly the most recent writes |

`never` exists for benchmarks and experiments. Do not use it for data you
care about.
