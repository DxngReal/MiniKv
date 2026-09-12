# MiniKV — Progress Tracker

## Current Status

- Project: MiniKV
- Current phase: Phase 4 — Interfaces
- Overall progress: 0 of 6 phases complete (Phases 1–4 implemented; race test pending toolchain)
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

Status: Not started

Scope:

- [ ] Complete unit + integration tests
- [ ] `go test -race ./...` clean
- [ ] Concurrent GET/SET/DELETE, TTL, snapshot tests
- [ ] Throughput / latency / allocation benchmarks
- [ ] WAL-enabled write benchmark, recovery time measurement
- [ ] Record real benchmark environment
- [ ] Write `docs/benchmarks.md`
- [ ] Fix all discovered races and reliability issues

Validation:

- gofmt: Not run
- go vet: Not run
- Unit tests: Not run
- Race tests: Not run
- Integration tests: Not run
- Benchmarks: Not run
- Build: Not run

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

- Project implementation has not started for phases 5–6.
- No performance numbers exist yet.
- No phase is verified complete.
- No release artifact exists.
- `go test -race` is blocked on this Windows machine by a missing C toolchain (cgo requirement); Phases 2–4 concurrency verified by tests only.
- Store.Get returns a read-only view of internal storage; callers must not mutate (documented in code).
- No automatic WAL compaction; recovery reports corruption instead of repairing.

---

## Session Handoff

- Date: 2026-09-12
- Phase: Phase 4 — Interfaces (Phases 1–4 implemented)
- Session goal: Complete Phase 4 HTTP API, CLI, graceful shutdown, integration tests
- Completed: Phases 1–3 committed earlier (7257c0d, 995621d, ff45b2f). Phase 4: api package (REST endpoints, JSON validation, consistent typed errors, status, snapshot endpoint, graceful shutdown), durable engine wrapper (WAL-first mutations, recovery on open, snapshot writes, WAL/snapshot stats), cli package (server + 6 client subcommands, flags anywhere, exit codes 0/1/2), full end-to-end integration suite
- Current milestone: Phase 4 done pending race evidence
- Files changed: internal/api/{server.go,handlers.go,handlers_test.go}, internal/cli/{server.go,client.go}, internal/engine/{engine.go,store.go,durable.go}, cmd/minikv/{main.go,main_test.go}, tests/{integration_test.go,helpers_test.go}, PROGRESS.md
- Commands actually run: `gofmt -w .`, `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `go test ./internal/api/ -count=1` (fixed leaked WAL handle), `go test ./tests/ -count=1 -v` (fixed .exe suffix and WALFileName reference)
- Test results: PASS — all packages ok with -count=1; 6 integration tests pass end to end
- Race test result: Not run — blocked: -race requires cgo, no C compiler on machine
- go vet result: PASS — no findings
- Benchmark result: Not run — benchmarks are Phase 5
- Build result: PASS — go build ./...
- Known limitations: race detector unusable on this machine; client commands need a running server; no WAL compaction
- Blockers: `go test -race` needs MinGW-w64 gcc (or equivalent) with CGO_ENABLED=1
- Next exact action: Begin Phase 5 (Quality and Benchmarks): add concurrent stress tests (GET/SET/DELETE/TTL/snapshot), benchmarks (SET/GET single-thread, mixed concurrent, TTL, WAL-enabled writes, recovery time), measure with real environment context, write docs/benchmarks.md, run go test -race when a toolchain is available
- Git commit: (this commit)
