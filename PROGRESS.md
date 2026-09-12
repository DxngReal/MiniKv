# MiniKV — Progress Tracker

## Current Status

- Project: MiniKV
- Current phase: Phase 6 — Release (final acceptance executed and passed)
- Overall progress: 6 of 6 phases complete
- Status: Complete pending final release commit
- Last updated: 2026-09-12
- Specification: `docs/MASTER_BUILD_PROMPT.md`
- Agent rules: `AGENTS.md`
- Context: `PROJECT_CONTEXT.md`

No phase may be marked complete without factual quality-gate evidence.

---

## Phase Overview

- [x] Phase 1 — Foundation
- [x] Phase 2 — Core Engine
- [x] Phase 3 — Persistence
- [x] Phase 4 — Interfaces
- [x] Phase 5 — Quality and Benchmarks
- [x] Phase 6 — Release

---

## Final Acceptance (2026-09-12)

Evidence gathered per `docs/MASTER_BUILD_PROMPT.md` §19 on this machine:

- Clean-clone workflow: `go build ./...`, `go test ./...` passed from the
  committed tree; `go vet ./...` exit 0; `gofmt -l .` empty.
- Race gate unblocked and passed: `go test -race -count=1 ./...` all
  packages ok, zero race reports. Toolchain: portable MinGW-w64 gcc 16.2.0
  (posix-seh, ucrt) on PATH with CGO_ENABLED=1. No system C compiler is
  installed; the toolchain lives in a temp directory and is not committed.
- HTTP endpoints verified against the compiled binary with curl on
  127.0.0.1:18080: SET 201, GET 200, GET missing 404 (typed JSON error with
  kind/message/hint), DELETE 204, keys listing, status (version, uptime,
  key_count, wal_bytes, durability_mode).
- TTL verified over HTTP: key with ttl_ms=1500 returned 200, then 404
  (key_not_found) after expiry.
- WAL recovery after abrupt termination verified: server killed with
  SIGKILL (not graceful shutdown); restart log shows recovery complete with
  snapshot_used=false, wal_records=4, applied_records=3, skipped_expired=1;
  the surviving key was readable after restart.
- Corrupted WAL verified: one byte flipped in wal.log; the server refused
  to start (exit code 1) with a typed wal_corruption error naming the file
  and byte offset; the WAL file's md5 was identical before the failed start
  and after it — never truncated, rewritten, or deleted.
- Snapshots verified: POST /v1/snapshot returned entries/bytes/duration and
  wrote snapshot.bin atomically; status then reported snapshot_entries=1;
  recovery log on the next start used the snapshot plus WAL ordering.
- Cross-platform builds verified: GOOS=linux/amd64 and GOOS=darwin/arm64
  binaries compiled with no toolchain beyond Go; README build instructions
  match what was run.
- Benchmarks re-run after the TTL test fix; results consistent with
  docs/benchmarks.md (Set 510.8 ns/op, Get hit 366.3 ns/op 0 allocs, WAL
  always 1.88 ms/op incl. fsync, Recovery10k 26.5 ms; environment: Intel
  Pentium Silver N5000 @ 1.10GHz, Windows, go1.27.1, -benchtime=1s).
- CI: `.github/workflows/ci.yml` checks gofmt, build, vet, test, and
  `go test -race` on ubuntu/windows/macos with Go 1.22. CI has not run on
  GitHub (no remote configured); the same commands were run locally and
  passed, including -race.
- Docker: Dockerfile reviewed (multi-stage golang:1.22-alpine → alpine:3.20,
  non-root user, healthcheck on /v1/status). Docker Desktop is installed but
  its daemon cannot start on this machine (Linux engine requires WSL2; no
  WSL distro is installed). Docker build therefore NOT verified locally.
  The identical build steps (CGO_ENABLED=0 go build -trimpath) were run
  successfully on the host.
- Repo hygiene: no WAL/snapshot/log artifacts or data directories tracked
  (git ls-files checked). No secrets or personal data committed.
- Documentation: README (features, architecture, install, cross-platform
  builds, Docker, usage, HTTP API, testing, recovery, limitations, security),
  docs/architecture.md, docs/design-decisions.md, docs/recovery.md,
  docs/benchmarks.md, docs/development.md, docs/roadmap.md, CONTRIBUTING.md,
  LICENSE (MIT) all present and consistent with the implementation.

Test fix this session (only code change): TestDurableRecoveryRestoresTTL in
internal/engine/durable_test.go was flaky under full-suite load — a 50 ms
TTL could legitimately lapse during a slow Close+Open cycle, so the
"restored" key was sometimes missing. The test now uses a 10 s TTL for the
restore check plus a deterministically lapsed 30 ms key to verify the
expired-record skip at recovery time. No production code changed; recovery
was verified correct (memory and WAL share the same absolute deadline in
DurableStore.Set).

---

## Phase 1 — Foundation

Status: Complete

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

Validation (final run 2026-09-12):

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — exit 0
- Unit tests: Passed — `go test ./... -count=1` all packages ok
- Race tests: Passed — `go test -race -count=1 ./...` ok, no findings (gcc 16.2.0 portable toolchain, CGO_ENABLED=1)
- Build: Passed — `go build ./...` exit 0

Known limitations: none for this phase.

---

## Phase 2 — Core Engine

Status: Complete

Scope:

- [x] In-memory key-value store (`internal/engine/store.go`: sharded Store, 32 default shards, FNV-1a routing)
- [x] GET / SET / DELETE (Engine interface, copy-on-Set, KeyNotFound/InvalidKey/InvalidValue/InvalidTTL kinds)
- [x] Key listing (Keys, sorted, expired keys never returned)
- [x] TTL expiration (lazy on access + janitor goroutine in ttl.go; expired behave as missing)
- [x] Sharded locking (one RWMutex per shard; never hold two shard locks; atomics for global counters)
- [x] Statistics (Stats: key count, puts, gets, hits, deletes, expired, shard count, uptime, HitRate)
- [x] Unit tests (CRUD, TTL lazy + janitor, validation, closed store, copy isolation, shard distribution, counters)
- [x] Race-safe implementation verified under the race detector

Validation (final run 2026-09-12):

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — exit 0
- Unit tests: Passed — all packages ok with -count=1
- Race tests: Passed — `go test -race -count=1 ./...` ok, no findings
- Build: Passed — exit 0

Known limitations:

- Get returns a slice aliasing internal storage (documented); Snapshot/Set copy.

---

## Phase 3 — Persistence

Status: Complete

Scope:

- [x] WAL format and framing (30-byte header: magic "KVMK", version, op, klen, vlen, expiration; little-endian)
- [x] CRC32 checksums (Castagnoli, bounds-checked lengths to prevent huge allocations)
- [x] WAL append and replay (Append flushes + fsyncs under DurabilityAlways; replay reports file and byte offset)
- [x] Snapshot creation (atomic) — temp file, fsync, close, rename, best-effort dir sync; failures leave the old snapshot intact
- [x] Crash recovery (Recover: latest snapshot, then all WAL records, expired SETs skipped and counted)
- [x] Corruption reporting (typed WALCorruption/SnapshotFailure errors with file + offset; ValidWALBytes exposes the intact prefix; files never modified or deleted)
- [x] Persistence tests (22 tests: roundtrip, 8 corruption cases, reopen-append, snapshot+>WAL ordering, torn tail, applier failure, idempotent close, bad data dir)

Validation (final run 2026-09-12):

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — exit 0
- Unit tests: Passed — all packages ok with -count=1
- Race tests: Passed — `go test -race -count=1 ./...` ok, no findings
- Crash-recovery tests: Passed — torn-tail and corrupted-byte scenarios verified; corrupted file byte-identical after recovery; also verified manually against the compiled binary (see Final Acceptance)
- Build: Passed — exit 0

Known limitations:

- No WAL compaction in v0.1.0 (documented in docs/design-decisions.md §5.3); WAL grows until the operator acts.
- Recovery is not linearizable with concurrent writes (recovery runs before the server accepts traffic; documented).

---

## Phase 4 — Interfaces

Status: Complete

Scope:

- [x] HTTP server + REST endpoints (internal/api: PUT/GET/DELETE /v1/keys/{key}, GET /v1/keys, GET /v1/status, POST /v1/snapshot; Go 1.22 method+wildcard routing)
- [x] JSON validation, consistent HTTP errors (error body: kind, message, hint; 400/404/405/500/503 mapping; body-size cap; ttl_ms validation incl. 0 = no expiry)
- [x] CLI commands (internal/cli + cmd/minikv: server, set, get, delete, keys, status, snapshot; flags before or after positionals; exit codes 0/1/2)
- [x] Graceful shutdown (SIGINT/SIGTERM → http.Shutdown → engine close; engine close verified in tests)
- [x] HTTP + CLI integration tests (tests/: real TCP server, full CRUD, TTL expiry, restart recovery, snapshot+>WAL ordering, corrupted-WAL reported and file untouched, compiled-binary CLI flow with exit codes)

Validation (final run 2026-09-12):

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — exit 0
- Unit tests: Passed — all packages ok with -count=1
- Race tests: Passed — `go test -race -count=1 ./...` ok, no findings
- Integration tests: Passed — tests/ suite ok (6 end-to-end tests)
- Manual HTTP verification: Passed — see Final Acceptance
- Build: Passed — exit 0

Known limitations:

- Client subcommands require a running server (by design); no embedded single-shot mode.
- POST /v1/snapshot requires the durable engine; plain in-memory engines return 503.

---

## Phase 5 — Quality and Benchmarks

Status: Complete

Scope:

- [x] Complete unit + integration tests (all packages; 6 end-to-end integration tests)
- [x] `go test -race ./...` clean — PASSED 2026-09-12 (portable MinGW-w64 gcc 16.2.0, CGO_ENABLED=1); zero race reports across all packages including the durable concurrency stress test
- [x] Concurrent GET/SET/DELETE, TTL, snapshot tests (TestDurableConcurrentMixedStress: 8 workers + snapshot loop + janitor against the durable engine; TestConcurrentAccess, TestConcurrentTTLWithSnapshot in the store)
- [x] Throughput / latency / allocation benchmarks (Set/Get hit/miss/Delete/Mixed/SetTTL/Snapshot with -benchmem)
- [x] WAL-enabled write benchmark, recovery time measurement
- [x] Record real benchmark environment (CPU, OS, Go version, storage, datasets, concurrency in docs/benchmarks.md)
- [x] Write `docs/benchmarks.md` (canonical measured run + interpretation + honest variance notes)
- [x] Fix all discovered issues found during stress testing: DurableStore.Set recomputes the absolute expiry deadline after the WAL append so memory and log can never diverge when fsync is slow
- [x] Fix flaky TestDurableRecoveryRestoresTTL (timing-dependent 50 ms TTL across restart; now deterministic — 2026-09-12)

Validation (final run 2026-09-12):

- gofmt: Passed — `gofmt -l .` empty
- go vet: Passed — exit 0
- Unit tests: Passed — all packages ok with -count=1
- Race tests: Passed — `go test -race -count=1 ./...` ok, no findings
- Integration tests: Passed — tests/ suite ok
- Benchmarks: Re-run — Set 510.8 ns/op; Get hit 366.3 ns/op (0 allocs); Get miss 6067 ns/op; Delete 1973 ns/op; Mixed 1696 ns/op; SetTTL 1090 ns/op; Snapshot 19.48 ms/op; WAL never 91.3 µs/op; WAL always 1.88 ms/op; Recovery10k 26.5 ms/op (Intel Pentium Silver N5000 @ 1.10GHz, Windows, go1.27.1, -benchtime=1s). Consistent with docs/benchmarks.md.
- Build: Passed — exit 0

Known limitations:

- Benchmark numbers are from a shared low-power CPU; variance between runs is large (documented in docs/benchmarks.md).

---

## Phase 6 — Release

Status: Complete

Scope:

- [x] GitHub Actions CI (fmt, vet, build, test, race on ubuntu/windows/macos, Go 1.22)
- [x] Dockerfile (multi-stage, CGO_ENABLED=0 static binary, non-root user, healthcheck)
- [x] Cross-platform build instructions (README; verified by compiling linux/amd64 and darwin/arm64 from this machine)
- [x] README completed (features, architecture, install, usage, HTTP API, testing, recovery, limitations, security, docs index)
- [x] docs: architecture, design-decisions, recovery, roadmap, development, benchmarks
- [x] LICENSE (MIT) + CONTRIBUTING.md (quality gate, house rules, PR process)
- [x] Final acceptance (see Final Acceptance section — executed 2026-09-12)
- [x] v0.1.0 release prepared

Validation (2026-09-12):

- CI: Workflow committed and reviewed; commands it runs were executed locally and passed (gofmt, build, vet, test, race). CI itself: Not run on GitHub — no remote repository is configured (`git remote -v` is empty).
- Docker build: Not run — Docker Desktop is installed but its daemon cannot start on this machine (Linux engine requires WSL2; no WSL distro installed). The Dockerfile's build steps (CGO_ENABLED=0 go build -trimpath -ldflags "-s -w") were verified on the host.
- Cross-platform builds: Passed — linux/amd64 and darwin/arm64 binaries compiled.
- Final acceptance: Passed — see the Final Acceptance section for per-item evidence.

Known limitations:

- CI has not executed on GitHub infrastructure (no remote configured).
- Docker image build not verified on this machine (daemon unavailable); Dockerfile follows the same verified build steps.

---

## Global Validation Summary (final, 2026-09-12)

- gofmt: Passed — `gofmt -l .` empty
- go vet ./...: Passed — exit 0
- go test ./...: Passed — all packages ok with -count=1
- go test -race ./...: Passed — all packages ok, zero race reports (portable gcc 16.2.0, CGO_ENABLED=1)
- Integration tests: Passed — tests/ suite inside `go test ./...`
- Benchmarks: Passed — real results in docs/benchmarks.md, re-confirmed this session
- go build ./...: Passed
- Cross-compilation: Passed — linux/amd64, darwin/arm64
- Docker build: Not run — daemon unavailable on this machine (WSL2 dependency)
- CI (GitHub): Not run — no remote configured; local equivalent commands all passed

---

## Known Limitations

- No automatic WAL compaction; recovery reports corruption instead of repairing (operator procedure documented in docs/recovery.md).
- Single node; no replication, HA, authentication, or TLS (documented everywhere).
- Store.Get returns a read-only view of internal storage; callers must not mutate (documented in code).
- Benchmark variance on a shared low-power CPU (documented in docs/benchmarks.md).
- CI-on-GitHub and Docker-image verification pending an environment with a remote and a working Docker daemon.

---

## Session Handoff

- Date: 2026-09-12
- Phase: Phase 6 — Release (final acceptance executed and passed)
- Session goal: verify Phase 6 artifacts, unblock and pass the race gate, execute the §19 final acceptance
- Completed: race gate unblocked via portable MinGW-w64 gcc 16.2.0 (CGO_ENABLED=1) — `go test -race -count=1 ./...` passed with zero race reports; flaky TestDurableRecoveryRestoresTTL made deterministic (test-only change); all §19 acceptance items verified manually against the compiled binary (HTTP CRUD + status, TTL expiry, SIGKILL recovery with stats, corrupted WAL refused with file+offset and file untouched, snapshot created and used on restart, cross-compilation, repo hygiene); PROGRESS.md and PROJECT_CONTEXT.md updated
- Files changed: internal/engine/durable_test.go (test fix), PROGRESS.md, PROJECT_CONTEXT.md
- Commands actually run: `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./... -count=1`, `go test ./internal/engine/ -count=3`, `go test -race -count=1 ./...`, `go test -bench=. -benchmem -benchtime=1s -run=^$ ./internal/engine/`, GOOS=linux/darwin cross-compiles, compiled-binary server + curl acceptance flow, `git status`, `git diff`, `git remote -v`, `git ls-files` hygiene check
- Test results: PASS — all packages ok (-count=1)
- Race test result: PASS — zero race reports
- go vet result: PASS — no findings
- Benchmark result: PASS — Set 510.8 ns/op; Get hit 366.3 ns/op (0 allocs); WAL always 1.88 ms/op; Recovery10k 26.5 ms; full run in docs/benchmarks.md context
- Build result: PASS — go build ./... plus linux/amd64 and darwin/arm64 cross-compiles
- Known limitations: Docker build and GitHub CI execution not verifiable on this machine (no WSL2 backend; no remote configured)
- Blockers: none for the code; Docker/CI verification requires a different environment
- Next exact action: final release-preparation review and commit; PROJECT COMPLETE can be reported with the documented caveat that CI-on-GitHub and the Docker image build were not machine-verified
- Git commit: (see final release commit)
