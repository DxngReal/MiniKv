# MiniKV

MiniKV is a small persistent concurrent key-value store written in Go.
It is an educational systems project: it demonstrates data structures,
concurrency, file I/O, write-ahead logging, crash recovery, HTTP
networking, CLI design, and testing — all with the standard library.

**Honest scope.** MiniKV is *not* a Redis replacement, not distributed,
and not highly available. It is a single-binary, single-node store built
to learn how these systems work.

> Status: feature-complete for v0.1.0 — all phases implemented; see
> `PROGRESS.md` for validation evidence.

## Development Workflow

Developed with an AI-assisted engineering workflow: a structured spec
(docs/MASTER_BUILD_PROMPT.md), agent execution rules (AGENTS.md), and
phase-based progress tracking (PROGRESS.md). Architecture decisions,
implementation review, testing, and validation were performed and verified
personally.

## Features

- `GET`, `SET`, `DELETE`, and key listing over an HTTP REST API
- Optional per-key TTL expiration
- Safe concurrent access via sharded locks
- Durable mutations through a checksummed write-ahead log (fsync per mutation)
- Atomic snapshots and crash recovery
- A single cross-platform binary with both server and client subcommands

## Architecture

```text
            ┌────────────┐
  HTTP ───► │  api       │───► engine.Engine (interface)
  CLI ────► │  cli       │        │
            └────────────┘        ▼
                           ┌────────────────┐
                           │ engine (store) │
                           └───────┬────────┘
                                   ▼
                           ┌────────────────┐
                           │  persistence   │  WAL + snapshots
                           └────────────────┘
```

The engine, persistence, api, and cli layers are separate packages with
explicit interfaces between them.

## Installation

Requires Go 1.22 or newer.

```bash
git clone <repository-url>
cd minikv
go build ./...
go install ./cmd/minikv
```

### Cross-platform builds

The binary is static; cross-compiling needs no toolchain beyond Go:

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/minikv-linux-amd64 ./cmd/minikv
GOOS=darwin  GOARCH=arm64 go build -o dist/minikv-darwin-arm64 ./cmd/minikv
GOOS=windows GOARCH=amd64 go build -o dist/minikv-windows-amd64.exe ./cmd/minikv
```

### Docker

```bash
docker build -t minikv .
docker run --rm -p 8080:8080 -v minikv-data:/data minikv \
  server --addr 0.0.0.0:8080 --data /data
```

## Usage

```bash
# Run the server (Ctrl+C for a graceful shutdown)
minikv server --addr 127.0.0.1:8080 --data ./data --durability always

# Client subcommands against a running server
minikv set greeting hello --ttl 60s
minikv get greeting
minikv delete greeting
minikv keys
minikv status
minikv snapshot
```

Exit codes: `0` success, `1` operation failure, `2` usage error.

## HTTP API

```http
PUT /v1/keys/greeting      {"value": "hello", "ttl_ms": 60000}   → 201/200
GET /v1/keys/greeting      → 200 {"key","value"} | 404
DELETE /v1/keys/greeting   → 204 | 404
GET /v1/keys               → {"keys": [...], "count": N}
GET /v1/status             → version, uptime, key count, WAL size, ...
POST /v1/snapshot          → snapshot statistics
```

- `ttl_ms` is optional; `ttl_ms: 0` means no expiration; negative values
  are rejected.
- Errors are consistent JSON objects with a machine-readable kind, a
  message, and a hint:

```json
{
  "error": "key_not_found",
  "message": "minikv: engine.Get: key_not_found: key does not exist. Next step: ...",
  "hint": "check the key name; create it with SET before reading it"
}
```

## Testing

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...      # requires a C toolchain (cgo) on Windows
go test -run Integration ./...
go test -bench=. -benchmem ./...
```

Measured benchmark results live in `docs/benchmarks.md`.

## Recovery behavior

Every mutation is appended to a checksummed WAL and fsynced before it is
acknowledged. After an abrupt termination, MiniKV reloads the latest valid
snapshot and replays the WAL. Corrupted WAL records are reported with file
and byte offset — never silently truncated or deleted. Details: `docs/recovery.md`.

## Limitations

- Single node only; no replication, clustering, or HA
- No authentication or TLS; bind to loopback if exposure matters
- Values are limited to 1 MiB and keys to 512 bytes
- Educational code: read it, break it, learn from it

## Security notes

- The HTTP API never exposes arbitrary filesystem paths.
- Keys and values are never logged.
- There is no authentication; do not expose MiniKV to untrusted networks.

## Documentation

- `CHANGELOG.md` — release history and notable changes
- `docs/architecture.md` — layers, dependency rules, write/recovery paths
- `docs/design-decisions.md` — why it looks this way + exact binary formats
- `docs/recovery.md` — operator guide for crashes and corruption
- `docs/benchmarks.md` — measured results with environment context
- `docs/development.md` — commands and conventions
- `docs/roadmap.md` — what's next, and what's deliberately out of scope

## Contributing

See `CONTRIBUTING.md`. Keep the engine, persistence, api, and cli package
boundaries clean, and run the full quality gate before proposing a change.

## License

MIT — see `LICENSE`.
