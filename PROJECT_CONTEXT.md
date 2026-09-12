# MiniKV — Project Context

## 1. Project Identity

- Project name: MiniKV
- Product type: Persistent concurrent key-value store (educational systems project)
- Language: Go 1.22+
- Platforms: Windows, macOS, Linux
- Backend requirement: None
- Core mode: single binary, stdlib-first, no external services
- Honest scope: NOT a Redis replacement, NOT distributed, NOT highly available

---

## 2. Current Status

- Current phase: Phase 6 — Release (all 6 phases complete; final acceptance passed 2026-09-12)
- Completed: 6 of 6
- Source of truth: `docs/MASTER_BUILD_PROMPT.md`
- Progress tracking: `PROGRESS.md` (per-phase evidence and final acceptance)
- Agent rules: `AGENTS.md`
- Quality gate: gofmt, go vet, go test, go test -race (zero reports), go build — all passing 2026-09-12
- Note: `go test -race` on Windows requires a C toolchain (cgo); verified with a portable MinGW-w64 gcc on PATH plus CGO_ENABLED=1

---

## 3. Commands

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test -run Integration ./...
go test -bench=. -benchmem ./...
go build ./...
go run ./cmd/minikv server --data ./data
```

Never claim a command passed unless actually executed and observed.

---

## 4. Conventions

- stdlib-first; new dependencies require justification
- engine / persistence / api / cli stay separated by package boundaries
- store values as bytes internally
- typed errors: KeyNotFound, InvalidKey, InvalidValue, InvalidTTL, WALCorruption, SnapshotFailure, RecoveryFailure
- never log key or value contents
- errors explain: what happened, why, what to do next
- tests: table-driven, temp dirs for integration, no personal data
- honesty rule: benchmark numbers must be real, measured, with machine context
