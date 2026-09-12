# Contributing to MiniKV

Thanks for helping! MiniKV is an educational project — clarity beats
cleverness.

## Development setup

- Go 1.22 or newer (`go version`)
- No third-party dependencies; stdlib only
- A C toolchain is needed for `go test -race` (cgo requirement on Windows)

## The quality gate (every change)

```bash
gofmt -w .            # format
go build ./...        # compile
go vet ./...          # static analysis
go test ./...         # unit + integration tests
go test -race ./...   # race detector (run before proposing a PR)
```

Benchmarks (only when touching hot paths):

```bash
go test -bench=. -benchmem ./internal/engine/
```

If you report benchmark numbers, include real machine context (CPU, OS, Go
version) as done in `docs/benchmarks.md`. Never invent numbers.

## House rules

- **Package boundaries:** `engine`, `persistence`, `api`, `cli` stay
  separated; persistence never imports transports; transports depend on
  the `engine.Engine` interface.
- **Errors:** return `*kverrors.Error` with a kind and a hint; never match
  on message strings; never swallow file/sync/close errors.
- **Data safety:** never silently truncate, repair, or delete corrupted
  WAL or snapshot data.
- **Secrets and personal data:** never log values, keys, tokens, or
  credentials; never commit local `data/`, `*.wal`, or `*.snapshot` files.
- **Tests:** table-driven where it helps; integration tests use
  `t.TempDir()`; run concurrency code under `-race`.
- **Docs:** update `PROGRESS.md` and relevant `docs/` pages with factual
  results when behavior changes.

## Pull requests

1. Branch from `master`.
2. One coherent change per PR, with tests.
3. Ensure the quality gate passes (CI will check on three OSes).
4. Describe the *why*, not just the *what*.

## License

By contributing you agree your contributions are licensed under the MIT
License in `LICENSE`.
