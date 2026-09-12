# Changelog

All notable changes to MiniKV are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-09-12

First release. A single-binary, single-node persistent concurrent
key-value store built entirely with the Go standard library. Educational
scope: not a Redis replacement, not distributed, not highly available.

Tag: `v0.1.0` · Commit: `517a851` (local repository; not published to a remote)

### Added

**Core engine** (`internal/engine`)

- In-memory key-value store sharded across 32 default shards with
  FNV-1a key routing and one `RWMutex` per shard; no shard ever holds
  two locks
- `GET` / `SET` / `DELETE` / key listing; keys are copied on `Set`,
  listing is sorted, expired keys are never returned
- Per-key TTL expiration (lazy on access + janitor goroutine)
- Statistics: key count, puts, gets, hits, deletes, expired, shard
  count, uptime, hit rate

**Persistence** (`internal/persistence`)

- Write-ahead log with a 30-byte frame header (magic `KVMK`, version,
  op, key/value lengths, expiration; little-endian) and CRC32
  (Castagnoli) checksums with bounds-checked lengths
- Durability modes: `always` (fsync per mutation, the default) and
  `never` (buffered)
- Atomic snapshots: temp file → fsync → close → rename → best-effort
  dir sync; a failed snapshot leaves the previous one intact
- Crash recovery: load the latest valid snapshot, replay all WAL
  records, skip expired `SET`s (counted), and report recovery
  statistics
- Corruption handling: typed `wal_corruption` / snapshot errors
  naming the file and byte offset; the intact prefix length is
  exposed; corrupted files are never truncated, rewritten, or deleted

**Interfaces**

- HTTP REST API (`internal/api`): `PUT/GET/DELETE /v1/keys/{key}`,
  `GET /v1/keys`, `GET /v1/status`, `POST /v1/snapshot`; consistent
  JSON errors with machine-readable kind, message, and hint; body-size
  cap; optional `ttl_ms` (`0` = no expiration, negative rejected)
- CLI (`internal/cli`, `cmd/minikv`): `server`, `set`, `get`, `delete`,
  `keys`, `status`, `snapshot`; flags accepted before or after
  positionals; exit codes `0` success / `1` failure / `2` usage error;
  server address configurable via `--addr`
- Graceful shutdown: SIGINT/SIGTERM → `http.Shutdown` → engine close
- Version reported as `0.1.0` by the CLI and `/v1/status`

**Foundation**

- Typed error model (`internal/kverrors`): 10 kinds with op, message,
  cause, and a "next step" hint
- Structured logging (`internal/logging`): slog JSON handler with
  level parsing; keys and values are never logged
- Configuration (`internal/config`): defaults, durability modes,
  validation; keys ≤ 512 bytes, values ≤ 1 MiB

**Quality and release**

- Unit tests for every package plus 6 end-to-end integration tests
  (real TCP server, CRUD, TTL expiry, restart recovery, corrupted-WAL
  handling, compiled-binary CLI flow)
- GitHub Actions CI (`.github/workflows/ci.yml`): gofmt, build, vet,
  test, and `go test -race` on ubuntu/windows/macos with Go 1.22
- Dockerfile: multi-stage `golang:1.22-alpine` → `alpine:3.20`,
  static `CGO_ENABLED=0` binary, non-root user, healthcheck on
  `/v1/status`
- Cross-platform builds verified: linux/amd64, darwin/arm64 (plus
  windows/amd64 as the development platform)

### Verified

All quality gates passed on the release machine (Intel Pentium Silver
N5000 @ 1.10 GHz, Windows, go1.27.1) on 2026-09-12 — evidence in
`PROGRESS.md`:

- `gofmt -l .` clean; `go vet ./...` exit 0; `go build ./...` exit 0
- `go test ./...` — all packages ok
- `go test -race ./...` — zero race reports (portable MinGW-w64
  gcc 16.2.0, CGO_ENABLED=1)
- Final acceptance per `docs/MASTER_BUILD_PROMPT.md` §19: HTTP
  SET/GET/DELETE/status, TTL expiry, WAL recovery after SIGKILL,
  corrupted WAL refused with file + offset (file byte-identical
  before and after), snapshot create + replay, CLI flow, Docker image
  built and verified serving traffic

Measured benchmarks with full environment context are in
`docs/benchmarks.md` (indicative numbers for this CPU class: Set
~370–510 ns/op, Get hit ~180–370 ns/op at 0 allocs, WAL append with
fsync ~1.0–1.9 ms/op, recovery of 10k WAL records ~15–27 ms).

### Known limitations

- No automatic WAL compaction; the log grows until the operator acts
  (procedure in `docs/recovery.md`)
- Recovery reports corruption instead of repairing; recovery runs
  before the server accepts traffic
- Single node: no replication, HA, authentication, or TLS — bind to
  loopback or put a proxy in front
- `Store.Get` returns a read-only view of internal storage; callers
  must not mutate it
- CI has not executed on GitHub infrastructure (no remote configured
  at release time); the identical commands pass locally

### Fixed (post-tag)

- CI: the workflow was pinned to Go 1.22, whose test binaries are aborted
  by the macOS 15 (arm64) loader at startup (`dyld: missing LC_UUID load
  command`) on `macos-latest` runners. CI now uses Go 1.27 — the toolchain
  the project is developed and verified with; `go.mod`'s language level
  (1.22) is unchanged.
