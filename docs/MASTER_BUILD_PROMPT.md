# MiniKV — Master Build Prompt

## 1. Role

You are a senior Go engineer, systems engineer, backend engineer, security
engineer, QA engineer, and technical writer.

Build MiniKV as a serious educational systems project for a first-year
computer science student. The code must be production-quality in structure,
testing, error handling, and documentation, while remaining honest about its
limited scope.

Do not copy Redis code or use an external key-value-store implementation.

---

## 2. Product Vision

MiniKV is a small persistent concurrent key-value store written in Go.

It demonstrates:

- Data structures
- Concurrency
- File I/O
- Write-ahead logging
- Crash recovery
- HTTP networking
- CLI design
- Testing and benchmarking

It is an educational project, not a Redis replacement or distributed database.

---

## 3. Core Requirements

MiniKV must:

- Run as a single cross-platform binary.
- Support `GET`, `SET`, `DELETE`, and key listing.
- Support optional TTL expiration.
- Be safe for concurrent access.
- Persist mutations through a WAL.
- Create and restore snapshots.
- Recover state after process interruption.
- Expose an HTTP REST API.
- Provide a CLI.
- Use no external runtime service.
- Never silently discard corrupted data.
- Report honest benchmark results.
- Work on Windows, macOS, and Linux where Go supports them.

---

## 4. Technology

Use:

- Go 1.22 or newer
- Standard library wherever practical
- `net/http`
- `sync`
- `encoding/json`
- `encoding/binary`
- `hash/crc32`
- `os`
- `bufio`
- `flag`
- `testing`

Testify is allowed for tests if already justified. Cobra is optional, but
prefer the standard `flag` package for the first release.

Do not add Redis clients, external key-value engines, or unnecessary libraries.

---

## 5. Architecture

Use clear package boundaries:

```text
minikv/
├── cmd/
│   └── minikv/
│       └── main.go
├── internal/
│   ├── engine/
│   │   ├── engine.go
│   │   ├── store.go
│   │   └── ttl.go
│   ├── persistence/
│   │   ├── wal.go
│   │   ├── snapshot.go
│   │   └── recovery.go
│   ├── api/
│   │   ├── server.go
│   │   ├── handlers.go
│   │   └── errors.go
│   ├── cli/
│   └── config/
├── tests/
├── docs/
├── .github/
│   └── workflows/
├── Dockerfile
├── go.mod
├── go.sum
├── README.md
├── LICENSE
└── .gitignore
```

The exact structure may differ if the existing repository has a justified
alternative.

### Engine

Define a small interface for:

- `Get`
- `Set`
- `Delete`
- `Keys`
- `Snapshot`
- `Stats`
- `Close`

Store values as bytes internally. HTTP and CLI layers may expose UTF-8 strings.

Use sharded maps or another clearly justified locking strategy. Each shard
must have its own mutex. Avoid one global lock unless there is a documented
reason.

Expired entries must behave as missing entries.

### Persistence

Use a framed WAL format with:

- Magic value
- Format version
- Operation type
- Key length
- Value length
- Expiration timestamp
- Key and value bytes
- CRC32 checksum

The exact binary layout must be documented in `docs/design-decisions.md`.

Snapshots must be written atomically:

1. Write to a temporary file.
2. Flush and `fsync`.
3. Close the file.
4. Rename it into place.
5. Optionally sync the parent directory where supported.

Do not expose unsafe arbitrary filesystem paths through the HTTP API.

---

## 6. HTTP API

Default server address:

```text
127.0.0.1:8080
```

Endpoints:

### Set

```http
PUT /v1/keys/{key}
Content-Type: application/json

{
  "value": "hello",
  "ttl_ms": 60000
}
```

- `ttl_ms` is optional.
- `ttl_ms: 0` means no expiration.
- Return `200` or `201` with JSON status.

### Get

```http
GET /v1/keys/{key}
```

- Return `200` with the value.
- Return `404` if missing or expired.

### Delete

```http
DELETE /v1/keys/{key}
```

- Return `204` when deleted.
- Return `404` if the key does not exist.

### List keys

```http
GET /v1/keys
```

Return keys only. Do not expose values.

### Status

```http
GET /v1/status
```

Include:

- Version
- Uptime
- Key count
- WAL size
- Snapshot status
- Persistence mode

Use consistent JSON errors. Validate method, path, body size, key length,
value size, malformed JSON, and invalid TTL values.

---

## 7. CLI

Use a single binary:

```bash
minikv server --addr 127.0.0.1:8080 --data ./data
minikv set hello world --ttl 60s
minikv get hello
minikv delete hello
minikv keys
minikv status
minikv snapshot
```

The CLI must provide:

- Helpful usage output
- Non-zero exit codes on failure
- Clear errors
- No secret leakage
- Configurable server address and data directory

---

## 8. WAL and Recovery Rules

- Every mutation must be appended to the WAL before being considered durable.
- The default durability mode must `fsync` each mutation.
- If batching is implemented later, it must be explicit and documented.
- WAL records must be checksummed.
- Recovery order is:
  1. Load the latest valid snapshot.
  2. Replay WAL records after the snapshot.
  3. Apply TTL rules.
  4. Report recovery statistics.
- A checksum mismatch, invalid frame, or truncated record must produce a clear
  recovery error containing the file and byte offset.
- Never silently truncate or delete corrupted WAL data.
- Provide an explicit, documented repair process if repair is implemented.
- Close files and flush pending data during graceful shutdown.

A process killed abruptly must be recoverable when the last completed WAL record
was durable.

---

## 9. Concurrency Rules

- The store must be safe for concurrent readers and writers.
- Run the race detector regularly.
- Avoid lock-order inversion.
- Do not call external or slow code while holding shard locks.
- Define lock ownership and ordering in code comments where necessary.
- TTL cleanup must not race with reads, writes, snapshots, or recovery.
- Tests must cover concurrent `GET`, `SET`, `DELETE`, expiration, and snapshot
  operations.

---

## 10. Testing

Write meaningful tests for:

- Basic CRUD
- Missing keys
- TTL expiration
- Invalid TTL values
- Concurrent access
- Shard behavior
- WAL encoding and checksums
- WAL replay
- Corrupt and truncated WAL records
- Snapshot atomicity
- Crash recovery
- HTTP handlers and status codes
- CLI exit codes
- Graceful shutdown
- Data-size and key validation

Required commands:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test -run Integration ./...
go test -bench=. -benchmem ./...
```

Add integration tests that use temporary directories. Tests must not use personal
data, real secrets, or fixed user paths.

---

## 11. Benchmarks

Provide benchmarks for:

- Single-threaded `SET`
- Single-threaded `GET`
- Mixed concurrent operations
- TTL operations
- WAL-enabled writes
- Recovery time for a known dataset

Report:

- Operations per second
- Latency where practical
- Allocation counts
- Dataset size
- CPU and machine context
- Number of goroutines
- Configuration

A target may be:

```text
More than 10,000 in-memory operations per second on ordinary laptop hardware.
```

This is only a target. Never claim performance numbers that were not measured.

Document real results in:

```text
docs/benchmarks.md
```

---

## 12. Error Handling

Errors must explain:

1. What happened.
2. Why it may have happened.
3. What the user can do next.

Use typed errors where helpful:

- Key not found
- Invalid key
- Invalid value
- Invalid TTL
- WAL corruption
- Snapshot failure
- Data-directory failure
- Recovery failure
- Server failure

Never ignore errors from file writes, sync, close, rename, or recovery.

---

## 13. Logging

Use structured, useful logs with levels such as:

- INFO
- WARN
- ERROR
- DEBUG

Log:

- Startup and shutdown
- Configuration summary
- Recovery results
- WAL and snapshot failures
- HTTP request failures where useful

Never log values, credentials, tokens, or full request bodies by default.

---

## 14. Documentation

Create:

```text
README.md
docs/architecture.md
docs/design-decisions.md
docs/benchmarks.md
docs/recovery.md
docs/development.md
docs/roadmap.md
```

The README must include:

- Project purpose
- Honest scope
- Feature list
- Architecture diagram
- Installation
- Usage examples
- HTTP API examples
- CLI examples
- Testing commands
- Benchmark commands
- Recovery behavior
- Limitations
- Security notes
- Contribution instructions
- License

Explain why each important design decision was made.

Do not claim MiniKV is production-ready, distributed, highly available, or
compatible with Redis.

---

## 15. Repository Hygiene

Include:

```text
.gitignore
.editorconfig
LICENSE
README.md
AGENTS.md
PROJECT_CONTEXT.md
PROGRESS.md
```

Do not commit:

```text
.env
*.db
*.wal
*.snapshot
data/
tmp/
coverage/
bin/
dist/
logs/
```

Do not commit benchmark output unless it is intentionally documented and
reproducible.

---

## 16. Implementation Phases

### Phase 1 — Foundation
- Go module, repository structure, configuration, error model, logging,
  engine interfaces, basic test setup, README skeleton, development commands

### Phase 2 — Core Engine
- In-memory store, GET/SET/DELETE/Keys, TTL expiration, sharded locking,
  statistics, unit tests, race-safe implementation

### Phase 3 — Persistence
- WAL format, CRC32 checksums, WAL append and replay, snapshots, atomic
  snapshot replacement, crash recovery, corruption reporting, persistence tests

### Phase 4 — Interfaces
- HTTP REST API, JSON validation, CLI commands, status endpoint, graceful
  shutdown, HTTP and CLI integration tests

### Phase 5 — Quality and Benchmarks
- Full unit tests, integration tests, `go test -race ./...`, `go vet ./...`,
  benchmarks, profiling where useful, `docs/benchmarks.md`, fix races and
  reliability issues

### Phase 6 — Release
- GitHub Actions CI, Dockerfile, cross-platform build instructions, final
  README, architecture and design docs, recovery documentation, license and
  contribution files, final acceptance, v0.1.0 release preparation

---

## 17. Agent Execution Rules

Before implementing:

1. Read `AGENTS.md`.
2. Read `PROJECT_CONTEXT.md`.
3. Read `PROGRESS.md`.
4. Read relevant sections of this prompt.
5. Inspect the repository and Git status.
6. Inspect `go.mod`, scripts, tests, and existing architecture.
7. Reuse working abstractions.
8. Implement one coherent milestone.

After every milestone:

1. Format Go code.
2. Run relevant tests.
3. Run `go vet ./...`.
4. Run `go test -race ./...`.
5. Update `PROGRESS.md` with factual results.
6. Review the diff.
7. Commit only clean, coherent work.

Never:

- Fabricate command results.
- Claim tests passed if they were not run.
- Disable the race detector to hide failures.
- Ignore persistence errors.
- Silently repair or delete corrupted data.
- Add unnecessary dependencies.
- Perform unrelated refactoring.
- Commit secrets or personal data.
- Push to a remote repository automatically.

Stop and ask the user only for:

- Architecture-level decisions
- Risky data migrations
- Major dependency changes
- Possible data loss
- Security-critical ambiguity
- Scope changes

---

## 18. Quality Gate

A phase is complete only when applicable requirements are verified:

```text
✓ Feature implemented
✓ Code formatted
✓ Unit tests pass
✓ Integration tests pass
✓ go vet ./... passes
✓ go test -race ./... passes
✓ Errors are handled
✓ Persistence behavior is tested
✓ Documentation is updated
✓ PROGRESS.md is updated
✓ Git diff is reviewed
✓ No known data-loss or security regression
```

If a command is unavailable or not run, record:

```text
Status: Not run
Reason:
Impact:
```

Do not mark the phase complete when required evidence is missing.

---

## 19. Final Acceptance

A clean checkout must support:

```bash
git clone <repository-url>
cd minikv
go build ./...
go test ./...
go test -race ./...
go run ./cmd/minikv server --data ./data
```

Then this flow must work:

```bash
minikv set greeting hello
minikv get greeting
minikv delete greeting
```

Verify:

- HTTP `SET`, `GET`, `DELETE`, and status work.
- TTL expires keys correctly.
- WAL recovery restores data after an abrupt process termination.
- Corrupted WAL data is reported, not silently deleted.
- Snapshots can be created and loaded.
- Benchmarks run and real results are documented.
- CI passes.
- Docker build works where supported.
- Cross-platform build instructions are accurate.
- README and design documentation are complete.
- No real secrets or personal data are committed.

Only after all evidence is verified may the project report:

```text
PROJECT COMPLETE
```

---

## 20. Session Workflow and Progress Tracking

Session continuity must not depend on chat history.

At the beginning of every session:

1. Read `AGENTS.md`.
2. Read `PROJECT_CONTEXT.md`.
3. Read `PROGRESS.md`.
4. Read only relevant sections of this prompt.
5. Inspect Git status and recent history.
6. Identify the current phase and next exact action.
7. Report the files read and planned scope.

Update `PROGRESS.md`:

- After every meaningful milestone.
- After tests, vet, race tests, or benchmarks.
- When a blocker is discovered.
- Before ending the session.
- Before committing.

At session end, record:

```md
## Session Handoff

- Date:
- Phase:
- Session goal:
- Completed:
- Files changed:
- Commands actually run:
- Test results:
- Race test result:
- Vet result:
- Benchmark result:
- Known limitations:
- Blockers:
- Next exact action:
- Git commit:
```

If context or time is running low, stop new work, finish a safe checkpoint,
update the handoff, review the diff, and commit locally. Do not push.
