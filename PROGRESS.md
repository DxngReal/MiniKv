# MiniKV — Progress Tracker

## Current Status

- Project: MiniKV
- Current phase: Phase 5 — Quality and Benchmarks
- Overall progress: 0 of 6 phases complete (Phases 1–5 implemented; race test pending toolchain)
- Status: In progress
- Last updated: 2026-09-12
- Specification: `docs/MASTER_BUILD_PROMPT.md`
- Agent rules: `AGENTS.md`
- Context: `PROJECT_CONTEXT.md`

No phase may be marked complete without factual quality-gate evidence.

---

## Phase Overview

- [ ] Phase 1 — Foundation (implementation complete; blocked on `go test -race` by missing C compiler)
- [ ] Phase 2 — Core Engine (implementation complete; same race-test blocker)
- [ ] Phase 3 — Persistence (implementation complete; same race-test blocker)
- [ ] Phase 4 — Interfaces (implementation complete; same race-test blocker)
- [ ] Phase 5 — Quality and Benchmarks (implementation complete; same race-test blocker)
- [ ] Phase 3 — Persistence
- [ ] Phase 4 — Interfaces
- [ ] Phase 5 — Quality and Benchmarks
- [ ] Phase 6 — Release

---

## Phase 1 — Foundation

Status: Implementation complete — race test pending (environment blocker)

Scope:

- [x] Initialize Go module (`go.mod`, module `minikv`, go 1.22; toolchain go1.27.1)
- [x] Create repository structure (`cmd/minikv`, `internal/{engine,kverrors,config,logging,version}`, `tests/`)
- [x] Define configuration (`internal/config`: defaults, durability modes, Validate)
- [x] Define typed error model (`internal/kverrors`: 10 kinds, Op/Message/cause/hint, IsKind/KindOf)
- [x] Add structured logging (`internal/logging`: slog JSON handler, level parsing)
- [x] Define engine interfaces (`internal/engine`: Engine, Stats, size limits, key/value/TTL validation)
- [x] Add initial test setup (unit tests per package; `tests/` integration harness sanity test)
- [x] Add README skeleton (honest scope, architecture, planned API/CLI, testing, recovery)
- [x] Add development documentation (`docs/development.md`)
- [x] Repository hygiene: `.gitignore`, `.editorconfig`, `LICENSE` (MIT)

Validation:

- gofmt: Passed — `gofmt -w .` run, no diffs afterward (all subsequent commands ran on formatted code)
- go vet: Passed — `go vet ./...`, exit 0, no findings
- Unit tests: Passed — `go test ./...`: ok for cmd/minikv, internal/config, internal/engine, internal/kverrors, internal/logging, tests
- Race tests: Not run — `go test -race ./...` fails to build: "-race requires cgo"; CGO_ENABLED=1 then fails with `gcc not found in %PATH%`. No C compiler (gcc/cc/clang) is installed on this Windows machine.
- Build: Passed — `go build ./...`, exit 0

Known limitations:

- `go test -race` cannot run on this machine until a C toolchain (e.g., MinGW-w64 gcc) is installed and CGO_ENABLED=1. Phase 1 contains no goroutines or shared mutable state, so exposure is minimal, but the phase checkbox stays unchecked until race evidence exists.
- Phase 4 commands (`server`, `set`, `get`, ...) intentionally exit 1 with a "planned for Phase 4" notice; only `version` and `help` are implemented.

---

## Phase 2 — Core Engine

Status: Implementation complete — race test pending (environment blocker, same as Phase 1)

Scope:

- [x] In-memory key-value store (`internal/engine/store.go`: sharded Store, 32 default shards, FNV-1a routing)
- [x] GET / SET / DELETE (Engine interface, copy-on-Set, KeyNotFound/InvalidKey/InvalidValue/InvalidTTL kinds)
- [x] Key listing (Keys, sorted, expired keys never returned)
- [x] TTL expiration (lazy on access + janitor goroutine in ttl.go; expired behave as missing)
- [x] Sharded locking (one RWMutex per shard; never hold two shard locks; atomics for global counters)
- [x] Statistics (Stats: key count, puts, gets, hits, deletes, expired, shard count, uptime, HitRate)
- [x] Unit tests (CRUD, TTL lazy + janitor, validation, closed store, copy isolation, shard distribution, counters)
- [x] Race-safe implementation (deferred race-detector run; concurrent tests written and passing without -race)

Validation:

- gofmt: Passed — `gofmt -w .` clean
- go vet: Passed — `go vet ./...` exit 0 (after removing an unused import caught by vet)
- Unit tests: Passed — `go test ./... -count=1` all 7 packages ok (one flaky Uptime assertion fixed with a 2ms sleep)
- Race tests: Not run — same environment blocker as Phase 1: `-race` requires cgo, no C compiler on this Windows machine. Concurrent tests (8 workers × mixed ops; TTL writer vs snapshot reader) pass without the race detector.
- Build: Passed — `go build ./...` exit 0

Known limitations:

- Race-detector evidence still pending for both phases; concurrency correctness is asserted by tests only.
- Get returns a slice aliasing internal storage (documented); Snapshot/Set copy.

---

## Phase 3 — Persistence

Status: Not started

Scope:

- [ ] WAL format and framing
- [ ] CRC32 checksums
- [ ] WAL append and replay
- [ ] Snapshot creation (atomic)
- [ ] Crash recovery
- [ ] Corruption reporting
- [ ] Persistence tests

Validation:

- gofmt: Not run
- go vet: Not run
- Unit tests: Not run
- Race tests: Not run
- Crash-recovery tests: Not run
- Build: Not run

## Phase 3 — Persistence

Status: Implementation complete — race test pending (environment blocker)

Scope:

- [x] WAL format and framing (30-byte header: magic "KVMK", version, op, klen, vlen, expiration; little-endian)
- [x] CRC32 checksums (Castagnoli, over everything after the magic; bounds-checked lengths to prevent huge allocations)
- [x] WAL append and replay (Append flushes + fsyncs under DurabilityAlways; replay reports file and byte offset)
- [x] Snapshot creation (atomic) — temp file, fsync, close, rename, best-effort dir sync; failures leave the old snapshot intact
- [x] Crash recovery (Recover: latest snapshot, then all WAL records, expired SETs skipped and counted)
- [x] Corruption reporting (typed WALCorruption/SnapshotFailure errors with file + offset; ValidWALBytes exposes the intact prefix; files never modified or deleted)
- [x] Persistence tests (22 tests: roundtrip, 8 corruption cases, reopen-append, snapshot+>WAL ordering, torn tail, applier failure, idempotent close, bad data dir)

Validation:

- gofmt: Passed — `gofmt -w .` clean (`gofmt -l .` empty)
- go vet: Passed — `go vet ./...` exit 0 (caught a missing test import; fixed)
- Unit tests: Passed — `go test ./... -count=1` all 8 packages ok (one test bug fixed: empty-key entry was rejected by Encode as designed)
- Race tests: Not run — same environment blocker as Phases 1–2 (cgo requires a C toolchain; none installed)
- Crash-recovery tests: Passed — torn-tail and corrupted-byte scenarios verified; corrupted file byte-identical after recovery
- Build: Passed — `go build ./...` exit 0

Known limitations:

- No WAL compaction in v0.1.0 (documented in docs/design-decisions.md §5.3); WAL grows until the operator acts.
- Recovery is not linearizable with concurrent writes (recovery runs before the server accepts traffic; documented).

---

## Phase 4 — Interfaces

Status: Implementation complete — race test pending (environment blocker)

Scope:

- [x] HTTP server + REST endpoints (internal/api: PUT/GET/DELETE /v1/keys/{key}, GET /v1/keys, GET /v1/status, POST /v1/snapshot; Go 1.22 method+wildcard routing)
- [x] JSON validation, consistent HTTP errors (error body: kind, message, hint; 400/404/405/500/503 mapping; body-size cap; ttl_ms validation incl. 0 = no expiry)
- [x] CLI commands (internal/cli + cmd/minikv: server, set, get, delete, keys, status, snapshot; flags before or after positionals; exit codes 0/1/2)
- [x] Graceful shutdown (SIGINT/SIGTERM → http.Shutdown → engine close; engine close verified in tests)
- [x] HTTP + CLI integration tests (tests/: real TCP server, full CRUD, TTL expiry, restart recovery, snapshot+>WAL ordering, corrupted-WAL reported and file untouched, compiled-binary CLI flow with exit codes)

Validation:

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — `go vet ./...` exit 0
- Unit tests: Passed — `go test ./... -count=1` all packages ok (test-only fixes: leaked WAL handle on Windows cleanup, .exe suffix for built binary, splitArgs for flags-after-positionals)
- Race tests: Not run — same environment blocker as Phases 1–3 (cgo requires a C toolchain; none installed)
- Integration tests: Passed — 6 end-to-end tests in tests/ including crash-recovery and corruption handling
- Build: Passed — `go build ./...` exit 0

Known limitations:

- Client subcommands require a running server (by design); no embedded single-shot mode.
- POST /v1/snapshot requires the durable engine; plain in-memory engines return 503.

---

---

## Phase 5 — Quality and Benchmarks

Status: Implementation complete — race test pending (environment blocker)

Scope:

- [x] Complete unit + integration tests (all packages; 6 end-to-end integration tests)
- [ ] `go test -race ./...` clean — BLOCKED: needs cgo C toolchain, none installed on this machine
- [x] Concurrent GET/SET/DELETE, TTL, snapshot tests (TestDurableConcurrentMixedStress: 8 workers + snapshot loop + janitor against the durable engine; TestConcurrentAccess, TestConcurrentTTLWithSnapshot in the store)
- [x] Throughput / latency / allocation benchmarks (Set/Get hit/miss/Delete/Mixed/SetTTL/Snapshot with -benchmem)
- [x] WAL-enabled write benchmark, recovery time measurement (DurabilityNever 42.8 µs/op, DurabilityAlways 1.34 ms/op incl. fsync, Recovery10k 22.0 ms)
- [x] Record real benchmark environment (CPU, OS, Go version, storage, datasets, concurrency in docs/benchmarks.md)
- [x] Write `docs/benchmarks.md` (canonical measured run + interpretation + honest variance notes)
- [x] Fix all discovered issues found during stress testing: DurableStore.Set now recomputes the absolute expiry deadline after the WAL append so memory and log can never diverge when fsync is slow; stress-test TTL usage corrected (fresh deadline per Set)

Validation:

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — `go vet ./...` exit 0
- Unit tests: Passed — `go test ./... -count=1` all packages ok
- Race tests: Not run — same environment blocker (cgo requires C toolchain); concurrency stress tests pass without -race and one divergence bug was caught and fixed by them
- Integration tests: Passed — tests/ suite ok
- Benchmarks: Run — real measured results recorded in docs/benchmarks.md with environment context
- Build: Passed — `go build ./...` exit 0

Known limitations:

- Race-detector evidence still pending across all phases; stress tests are the interim concurrency evidence.
- Benchmark numbers are from a shared low-power CPU; variance between runs is large (documented).

---

## Phase 6 — Release

Status: Not started

Scope:

- [ ] GitHub Actions CI (fmt, vet, tests, race)
- [ ] Dockerfile
- [ ] Cross-platform build instructions
- [ ] README completed
- [ ] docs: architecture, design-decisions, recovery, roadmap
- [ ] LICENSE + CONTRIBUTING.md
- [ ] Final acceptance
- [ ] v0.1.0 release prepared

Validation:

- CI: Not run
- Docker build: Not run
- Final acceptance: Not run
- Release build: Not run

---

## Global Validation Summary

- gofmt: Passed (2026-09-12, after each milestone)
- go vet ./...: Passed (2026-09-12)
- go test ./...: Passed (2026-09-12) — all 7 packages ok, -count=1
- go test -race ./...: Not run — requires cgo; no C compiler installed (gcc not found). Impact: concurrency correctness not machine-verified for the Phase 2 store and Phase 3 WAL/snapshot code. Next: install MinGW-w64 or validate on a machine with a C toolchain.
- Integration tests: Not applicable yet (harness sanity test runs inside `go test ./...`)
- Benchmarks: Not run (no benchmarks exist yet)
- go build ./...: Passed (2026-09-12)
- Docker build: Not run (no Dockerfile yet)
- CI: Not run (no workflow yet)

---

## Known Limitations

- Project implementation has not started for phase 6.
- No performance numbers exist yet.
- `go test -race` is blocked on this Windows machine by a missing C toolchain (cgo requirement); Phases 2–4 concurrency verified by tests only.
- Store.Get returns a read-only view of internal storage; callers must not mutate (documented in code).
- No automatic WAL compaction; recovery reports corruption instead of repairing.

---

- Benchmark numbers exist for the in-memory store, WAL modes, and recovery; see docs/benchmarks.md.

## Session Handoff

- Date: 2026-09-12
- Phase: Phase 5 — Quality and Benchmarks (Phases 1–5 implemented)
- Session goal: Complete Phase 5 stress tests, benchmarks, docs/benchmarks.md
- Completed: Phases 1–4 committed earlier (7257c0d, 995621d, ff45b2f, 1435476). Phase 5: engine + durable benchmarks (Set/Get/miss/Delete/Mixed/SetTTL/Snapshot, WAL never/always, Recovery10k), durable concurrency stress tests, docs/benchmarks.md with real environment context, TTL-divergence fix in DurableStore.Set
- Current milestone: Phase 5 done pending race evidence
- Files changed: internal/engine/{bench_test.go,durable_test.go} (new), internal/engine/durable.go (TTL fix), docs/benchmarks.md (new), PROGRESS.md
- Commands actually run: `gofmt -w .`, `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `go test ./internal/engine/ -count=1`, `go test -bench=. -benchmem -benchtime=1s -run=^$ ./internal/engine/` (twice: pre- and post-fix)
- Test results: PASS — all packages ok with -count=1
- Race test result: Not run — blocked: -race requires cgo, no C compiler on machine
- go vet result: PASS — no findings
- Benchmark result: Measured — Set 1,326 ns/op; Get hit 356 ns/op (0 allocs); mixed 8-goroutine 506 ns/op; WAL always 1.34 ms/op (fsync per mutation); recovery 10k records 22.0 ms; full table in docs/benchmarks.md
- Build result: PASS — go build ./...
- Known limitations: race detector unusable on this machine; benchmark variance on shared low-power CPU (documented)
- Blockers: `go test -race` needs MinGW-w64 gcc (or equivalent) with CGO_ENABLED=1
- Next exact action: Begin Phase 6 (Release): GitHub Actions CI workflow, Dockerfile, cross-platform build instructions, complete README + docs (architecture, recovery, roadmap), CONTRIBUTING.md, final acceptance pass per MASTER_BUILD_PROMPT §19
- Git commit: (this commit)
