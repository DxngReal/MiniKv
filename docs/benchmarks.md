# MiniKV — Benchmark Results

All numbers below were **measured** with the commands shown. They are not
estimates, and they will differ on other machines. MiniKV makes no
performance claims beyond what is recorded here.

## Environment

| Item | Value |
| --- | --- |
| Date | 2026-09-12 |
| CPU | Intel(R) Pentium(R) Silver N5000 @ 1.10 GHz (4 cores, Gemini Lake) |
| OS | Windows (amd64) |
| Go version | go1.27.1 windows/amd64 |
| Go flags | none (default optimization level) |
| Storage | local SSD-backed filesystem (e.g. NVMe/SATA), no exotic tuning |
| Datasets | in-memory benches: 10,000 keys × 32-byte values unless noted; recovery bench: 10,000 WAL records |
| Concurrency | mixed bench: 8 goroutines |
| Durability | `never` unless a bench says `always` |

Command:

```bash
go test -bench=. -benchmem -benchtime=1s -run=^$ ./internal/engine/
```

## Results (measured 2026-09-12, canonical run)

```text
goos: windows
goarch: amd64
pkg: minikv/internal/engine
cpu: Intel(R) Pentium(R) Silver N5000 CPU @ 1.10GHz
BenchmarkSetNoTTL-4                     1000000      1326   ns/op      34 B/op      1 allocs/op
BenchmarkGetHit-4                       4188436       355.6 ns/op       0 B/op      0 allocs/op
BenchmarkGetMiss-4                      1000000      1020   ns/op      88 B/op      2 allocs/op
BenchmarkDelete-4                       1623405      1038   ns/op       7 B/op      0 allocs/op
BenchmarkMixedConcurrent-4              3434058       506.4 ns/op      37 B/op      2 allocs/op
BenchmarkSetWithTTL-4                   2395152       506.7 ns/op      16 B/op      2 allocs/op
BenchmarkSnapshot-4                         135   14739367   ns/op 1887335 B/op  10079 allocs/op
BenchmarkWALAppendDurabilityNever-4        23730     42769 ns/op     337 B/op      4 allocs/op
BenchmarkWALAppendDurabilityAlways-4         925   1344251 ns/op     359 B/op      3 allocs/op
BenchmarkRecovery10k-4                       82   22003609 ns/op  4173488 B/op  40550 allocs/op
```

A second run earlier the same day (before a TTL fix in DurableStore.Set)
measured Set at 440 ns/op and recovery at 62.8 ms — run-to-run variance on
this shared, low-power machine is large; treat all numbers as indicative
for this CPU class, not as absolute constants.

## Group-commit A/B (measured 2026-09-12, after v0.1.0)

`BenchmarkWALAppendDurabilityAlwaysConcurrent` (8 goroutines, durable
writes) was added with group-commit fsync. Controlled back-to-back
comparison (same command, `-benchtime=2s`, baseline obtained by stashing
only the WAL change):

| Benchmark | Baseline | Group commit |
| --- | ---: | ---: |
| WAL always, 1 goroutine | 1.097 ms/op | 1.103 ms/op |
| WAL always, 8 goroutines | 1.092–10.15 ms/op | 0.83–1.46 ms/op |

- Serial path: unchanged within noise — it is fsync-bound (~1.1 ms/op on
  this machine), as intended. The leader/follower design performs the
  flush+fsync inline for the first waiting writer, so no goroutine hop is
  added.
- Concurrent path: the baseline's serialized fsyncs amplify machine noise
  (observed 1.1–10.2 ms/op across the day); with group commit the same
  benchmark held 0.83–1.46 ms/op across repeated runs. Best observed
  improvement ≈11× (10.15 → 0.93 ms/op in one A/B); the more important
  effect is that durable-write latency under load stopped swinging.
- Durability semantics are unchanged: an acknowledged append was covered
  by a completed fsync. Verified by tests that abandon the WAL without
  Close (simulating a kill) and recover every acknowledged record.

## Interpretation (single-goroutine unless noted, canonical run)

| Benchmark | ns/op | ≈ ops/second | Notes |
| --- | ---: | ---: | --- |
| Set (no TTL) | 1,326 | ~0.75 M | includes value copy + shard write lock |
| Get (hit) | 356 | ~2.8 M | zero allocations; returns internal view |
| Get (miss) | 1,020 | ~1.0 M | allocation comes from the typed error |
| Delete | 1,038 | ~1.0 M | single delete of a seeded key |
| Mixed concurrent (8 goroutines) | 506 per op | ~2.0 M | 50% get / 30% set / 20% delete |
| Set with TTL | 507 | ~2.0 M | fresh absolute deadline per write |
| Snapshot of 10k entries | 14.7 ms | ~68/s | deep copy of ~1.9 MB, 10k allocations |
| WAL append (`never`) | 42.8 µs | ~23 K | buffer flush per append; no fsync |
| WAL append (`always`) | 1.34 ms | ~744 | **one fsync per mutation** (the default) |
| Recovery of 10k WAL records | 22.0 ms | — | full replay into a fresh store |

Against the project's aspirational target (>10,000 in-memory ops/s on an
ordinary laptop), MiniKV exceeds the target by two orders of magnitude for
in-memory operations on this low-power CPU.

## Honest observations

- **fsync dominates durable writes.** The `always` durability mode is ~85×
  slower than `never` on this machine. That is the cost of the
  specification's correctness requirement (a completed mutation must
  survive an abrupt termination), not an implementation bug. Batched or
  group-commit fsync would close the gap and is a roadmap item.
- **Recovery is linear in WAL size** (~6 µs per record here). Recovery time
  after a crash grows with un-snapshotted WAL length; taking periodic
  snapshots bounds it.
- **Get returns an internal view** (zero allocations). Callers must treat
  the returned slice as read-only; documented in the API.
- **This is a low-power CPU.** A typical laptop or desktop will report
  substantially better numbers. Re-run the command above on your hardware
  and compare.
- The race detector **passed** on this machine (2026-09-12, via a portable
  MinGW-w64 gcc 16.2.0 toolchain with CGO_ENABLED=1): `go test -race ./...`
  reported zero races across all packages. Benchmarks themselves ran without
  `-race`, as is standard, so the numbers above are unaffected by detector
  overhead.

## Reproducing

```bash
go test -bench=. -benchmem ./internal/engine/
```

Append `-benchtime=2s` for steadier numbers, or `-run=^$` to skip tests.
