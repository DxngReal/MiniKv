# MiniKV — Progress Tracker

## Current Status

- Project: MiniKV
- Current phase: Phase 6 — Release (final acceptance executed and passed; re-verified 2026-09-12)
- Overall progress: 6 of 6 phases complete
- Status: Complete — PROJECT COMPLETE reported after re-verification (sessions 2 and 3)
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
  Re-confirmed again in the second session (see Re-verification above).
- CI: `.github/workflows/ci.yml` checks gofmt, build, vet, test, and
  `go test -race` on ubuntu/windows/macos with Go 1.22. CI has not run on
  GitHub (no remote configured); the same commands were run locally and
  passed, including -race, in both sessions.
- Docker: VERIFIED 2026-09-12 (second session) — daemon available
  (Docker 29.7.2); `docker build -t minikv:accept .` succeeded and the
  container served /v1/status, SET 201, and GET 200 on 127.0.0.1:18099.
  Dockerfile reviewed (multi-stage golang:1.22-alpine → alpine:3.20,
  non-root user, healthcheck on /v1/status).
- Repo hygiene: no WAL/snapshot/log artifacts or data directories tracked
  (git ls-files checked). No secrets or personal data committed.
- Documentation: README (features, architecture, install, cross-platform
  builds, Docker, usage, HTTP API, testing, recovery, limitations, security),
  docs/architecture.md, docs/design-decisions.md, docs/recovery.md,
  docs/benchmarks.md, docs/development.md, docs/roadmap.md, CONTRIBUTING.md,
  LICENSE (MIT) all present and consistent with the implementation.

Re-verification (2026-09-12, second runner session) — full gate and
acceptance re-executed from the committed tree:

- Quality gate re-run and passed: `gofmt -l .` empty; `go vet ./...` exit 0;
  `go build ./...` exit 0; `go test -count=1 ./...` all packages ok.
- Race gate re-provisioned and re-run: portable MinGW-w64 gcc 16.2.0
  (sha256-verified, temp dir, not committed) + CGO_ENABLED=1;
  `go test -race -count=1 ./...` all packages ok, zero race reports.
- Compiled binary re-verified over HTTP on 127.0.0.1:18081-18084: SET 201,
  GET 200, GET missing 404 (typed JSON error), DELETE 204, keys, status
  (version, uptime, key_count, wal_bytes, durability_mode).
- TTL over HTTP re-verified: ttl_ms=1500 key returned 200 then 404
  (key_not_found) after expiry.
- SIGKILL recovery re-verified: server killed (not graceful); restart log:
  snapshot_used=true, snapshot_entries=2, wal_records=6, applied_records=7,
  skipped_expired=1; keys a, b, c all present (snapshot + WAL ordering).
- Corrupted-WAL refusal re-verified: 4 bytes overwritten at offset 64 via
  dd; start attempt exited 1 with a typed wal_corruption error naming
  wal.log and offset 47; md5 of wal.log identical before the failed start
  and after it — never truncated, rewritten, or deleted. WAL then restored
  from backup and the server started cleanly.
- Snapshot re-verified: POST /v1/snapshot returned entries/bytes/duration/
  path and wrote snapshot.bin; a key written after the snapshot survived
  restart via WAL replay.
- CLI flow re-verified against a live server with the documented `--addr`
  flag: set/get/keys/status/snapshot/delete, non-zero exit on missing key,
  exit 2 + usage on unknown command. Note: the CLI's server address is
  configurable via `--addr` (as specified); there is no MINIKV_ADDR
  environment variable, so clients must pass --addr when the server is not
  on the default port.
- Docker verified for the first time on this machine: the daemon became
  available (Docker 29.7.2); `docker build -t minikv:accept .` succeeded and
  the container was run (`docker run -d -p 127.0.0.1:18099:8080`), serving
  /v1/status, SET (201) and GET (200) from inside the container.
- Cross-platform builds re-verified: GOOS=linux/amd64 and GOOS=darwin/arm64
  compiled.
- Benchmarks re-run: Set 366.7 ns/op, Get hit 179.1 ns/op (0 allocs),
  Delete 989 ns/op, Mixed 417 ns/op, SetTTL 533 ns/op, Snapshot 5.9 ms,
  WAL never 20.5 µs, WAL always 1.02 ms, Recovery10k 14.5 ms — same orders
  of magnitude as the canonical run in docs/benchmarks.md; variance on this
  shared low-power CPU remains documented there.
- Release polish (only code/doc changes this session): version stamped
  0.1.0 in internal/version (Phase 6 scope: "v0.1.0 release prepared";
  no test asserts the -dev string); docs/benchmarks.md stale claim that the
  race detector was unavailable replaced with the factual passing result.
- CI on GitHub: still not executed — no remote repository is configured
  (`git remote -v` empty); the workflow's commands all pass locally.

Test fix earlier on 2026-09-12 (previous session): TestDurableRecoveryRestoresTTL in
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
- Docker build: Passed — VERIFIED 2026-09-12 (second session): daemon available (Docker 29.7.2); `docker build -t minikv:accept .` succeeded and the container served /v1/status, SET 201, and GET 200 on 127.0.0.1:18099.
- Cross-platform builds: Passed — linux/amd64 and darwin/arm64 binaries compiled.
- Final acceptance: Passed — see the Final Acceptance section for per-item evidence.

Known limitations:

- CI has not executed on GitHub infrastructure (no remote configured).
- Docker image build was unverified until the second session on 2026-09-12, when the daemon became available and the image was built and run successfully (see Final Acceptance → Re-verification).

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
- Docker build: Passed — verified 2026-09-12 (second session): image built, container served traffic
- CI (GitHub): Not run — no remote configured; local equivalent commands all passed

---

## Known Limitations

- No automatic WAL compaction; recovery reports corruption instead of repairing (operator procedure documented in docs/recovery.md).
- Single node; no replication, HA, authentication, or TLS (documented everywhere).
- Store.Get returns a read-only view of internal storage; callers must not mutate (documented in code).
- Benchmark variance on a shared low-power CPU (documented in docs/benchmarks.md).
- CI has never executed on GitHub infrastructure — no remote repository is configured; the identical commands pass locally on Windows.
- The CLI has no MINIKV_ADDR environment variable; the server address is set with `--addr` (spec-compliant).

---

## Session Handoff

- Date: 2026-09-12 (third runner session)
- Phase: Phase 6 — Release (PROJECT COMPLETE state maintained and re-verified)
- Session goal: re-run the full quality gate from the committed tree and spot-verify acceptance evidence (binary HTTP flow, Docker image from HEAD)
- Completed: gofmt/vet/build all clean; `go test -count=1 ./...` all packages ok; race gate re-run with the cached portable MinGW-w64 gcc 16.2.0 (temp dir, sha256-verified in the second session) — zero race reports; v0.1.0 binary verified serving HTTP (status shows version 0.1.0; SET 201 / GET 200 / DELETE 204); Docker image rebuilt from HEAD and container verified serving version 0.1.0 with a successful SET; no stray processes or containers left behind
- Files changed: PROGRESS.md only
- Commands actually run: `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./... -count=1`, `go test -race -count=1 ./...` (CGO_ENABLED=1, portable gcc), `go build -o` binary + server + curl smoke flow, `docker build`, `docker run` + HTTP check, `tasklist`/`docker ps` stray-process check, `git status`, `git log`, `git remote -v`, `git tag`
- Test results: PASS — all packages ok (-count=1)
- Race test result: PASS — zero race reports
- go vet result: PASS — no findings
- Benchmark result: Not run this session — prior sessions' runs (2026-09-12) are recorded in docs/benchmarks.md and the Global Validation Summary; no code changed since
- Build result: PASS — go build ./...; Docker image from HEAD serves v0.1.0
- Known limitations: CI has never executed on GitHub (no remote configured); benchmark variance on shared low-power CPU
- Blockers: none
- Next exact action: none — project remains complete; optional follow-ups are tagging v0.1.0 and adding a remote so CI can run
- Git commit: e5dd451 (final release commit; any PROGRESS.md-only update committed separately)
