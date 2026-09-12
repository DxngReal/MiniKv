# MiniKV — Development Guide

This document collects the commands and conventions used while building
MiniKV. The authoritative specification is `docs/MASTER_BUILD_PROMPT.md`;
factual progress lives in `PROGRESS.md`.

## Requirements

- Go 1.22 or newer (`go version`)
- No external runtime services; no third-party Go dependencies so far

## Commands

```bash
# Format all Go code in place
gofmt -w .

# Static analysis
go vet ./...

# Build every package
go build ./...

# Unit tests
go test ./...

# Tests with the race detector (run regularly)
go test -race ./...

# Integration tests only
go test -run Integration ./...

# Benchmarks with allocation stats
go test -bench=. -benchmem ./...
```

## Package layout

```text
cmd/minikv/          single binary: server + client subcommands
internal/engine/     storage contract, limits, stats (implementation in Phase 2)
internal/kverrors/   typed error model shared by all layers
internal/config/     configuration types, defaults, validation
internal/logging/    slog-based structured logging
internal/version/    version constant
internal/persistence/ WAL + snapshots (Phase 3)
internal/api/        HTTP REST API (Phase 4)
internal/cli/        client subcommands (Phase 4)
tests/               cross-package integration tests
docs/                specification, architecture, benchmarks, recovery
```

## Conventions

- stdlib-first: new dependencies require justification
- engine / persistence / api / cli stay separated by package boundaries
- values are stored as bytes internally; HTTP and CLI may expose UTF-8 strings
- typed errors via `internal/kverrors` with kinds and actionable hints
- errors explain what happened, why, and what to do next
- never log key or value contents, secrets, or tokens
- tests are table-driven; integration tests use temporary directories
- every milestone: gofmt → tests → vet → race tests → update PROGRESS.md → commit

## Error model

All MiniKV errors are `*kverrors.Error` values carrying a `Kind`, an `Op`,
a message, an optional wrapped cause, and a hint. Match errors with
`kverrors.IsKind(err, kverrors.KeyNotFound)` — never on message strings.

Kinds: `key_not_found`, `invalid_key`, `invalid_value`, `invalid_ttl`,
`wal_corruption`, `snapshot_failure`, `recovery_failure`, `data_dir_failure`,
`server_failure`, `store_closed`.

## Logging

Structured JSON via `log/slog` (`internal/logging.New`). Levels: debug,
info, warn, error (default info). Never log values, credentials, or request
bodies.

## Durability

Default durability is fsync-per-mutation (`config.DurabilityAlways`).
`DurabilityNever` exists for tests and experiments only and must never
become the default.
