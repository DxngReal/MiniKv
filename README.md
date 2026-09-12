# MiniKV

MiniKV is a small persistent concurrent key-value store written in Go.
It is an educational systems project: it demonstrates data structures,
concurrency, file I/O, write-ahead logging, crash recovery, HTTP
networking, CLI design, and testing — all with the standard library.

**Honest scope.** MiniKV is *not* a Redis replacement, not distributed,
and not highly available. It is a single-binary, single-node store built
to learn how these systems work.

> Status: under active development — Phase 1 (Foundation) in progress.
> The HTTP API and CLI arrive in Phase 4; the sections below describe the
> finished v0.1.0 behavior.

## Planned features

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

## Usage

```bash
# Run the server (Phase 4)
minikv server --addr 127.0.0.1:8080 --data ./data

# Client subcommands (Phase 4)
minikv set greeting hello --ttl 60s
minikv get greeting
minikv delete greeting
minikv keys
minikv status
minikv snapshot
```

Right now only `minikv version` and `minikv help` are implemented.

## HTTP API (Phase 4)

```http
PUT /v1/keys/greeting      {"value": "hello", "ttl_ms": 60000}
GET /v1/keys/greeting      → 200 "hello" | 404
DELETE /v1/keys/greeting   → 204 | 404
GET /v1/keys               → list of keys
GET /v1/status             → version, uptime, key count, WAL size, ...
```

Errors are consistent JSON objects with a machine-readable kind and a
human-readable hint.

## Testing

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test -run Integration ./...
go test -bench=. -benchmem ./...
```

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

## Contributing

See `CONTRIBUTING.md` (Phase 6). Until then: fork, branch, test, and
open a pull request. Keep the engine, persistence, api, and cli package
boundaries clean.

## License

MIT — see `LICENSE`.
