# MiniKV Agent Instructions

You are working on MiniKV, a persistent concurrent key-value store written in Go.

## Required Session Startup

Before inspecting or modifying code, read:

1. `AGENTS.md`
2. `PROJECT_CONTEXT.md`
3. `PROGRESS.md`
4. Relevant sections of `docs/MASTER_BUILD_PROMPT.md`

Do not print the full master prompt. Summarize only relevant information.

## Source of Truth

- `docs/MASTER_BUILD_PROMPT.md`: complete product specification
- `PROJECT_CONTEXT.md`: architecture, conventions, and project context
- `PROGRESS.md`: factual progress and session handoff

The current phase is recorded in `PROGRESS.md`. Work only on that phase unless
a dependency requires otherwise.

## Go Execution Rules

Before changing code:

```bash
git status
go version
```

Use these commands where applicable:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test -run Integration ./...
go test -bench=. -benchmem ./...
go build ./...
```

Rules:

- Never commit failing tests.
- Never disable the race detector to hide a race.
- Never fabricate command results.
- Never claim a command passed unless it was actually run.
- Treat WAL, snapshot, fsync, rename, and recovery errors seriously.
- Never silently truncate, delete, or repair corrupted data.
- Never copy Redis code or use an external KV implementation.
- Keep dependencies minimal.
- Benchmark results must include real machine and configuration context.
- Do not log values, secrets, tokens, or credentials.
- Do not commit personal data or local database files.
- Do not push to a remote repository automatically.

## Architecture Rules

- Keep the engine independent from HTTP and CLI code.
- Keep persistence independent from transport layers.
- Use explicit interfaces and typed errors.
- Avoid holding locks while performing slow filesystem or network operations.
- Document lock ownership and ordering.
- Preserve concurrency safety during snapshots, recovery, and TTL cleanup.
- Reuse existing abstractions.
- Avoid unrelated refactoring.

## Milestone Protocol

For each coherent milestone:

1. Inspect existing code and tests.
2. Implement the smallest complete change.
3. Add meaningful tests.
4. Run `gofmt`.
5. Run relevant tests.
6. Run `go vet ./...`.
7. Run `go test -race ./...`.
8. Run benchmarks when relevant.
9. Update `PROGRESS.md` with factual results.
10. Review `git diff`.
11. Commit clean work locally with a clear message.

## Progress Protocol

Update `PROGRESS.md`:

- At session start if the current state is unclear.
- After every meaningful milestone.
- After every validation command.
- When a blocker or next action changes.
- Before ending the session.

Write `Not run` when a command was not run.
Write `Not available` when a script or command does not exist.

Never mark a phase complete without quality-gate evidence.

## Session Start Response

Before implementation, report:

```text
Files read:
Current phase:
Repository status:
Go version:
Existing relevant implementation:
Planned scope:
Potential blockers:
```

Then provide a short plan and continue.

## Session End Response

Before ending:

```text
Implemented:
Files changed:
Commands actually run:
Tests:
Race test:
go vet:
Benchmarks:
Known limitations:
Blockers:
Next exact action:
Git commit:
```

If context or session capacity is low:

1. Stop starting new work.
2. Finish the current safe checkpoint.
3. Run applicable validation.
4. Update `PROGRESS.md`.
5. Review the diff.
6. Commit clean changes locally.
7. Do not push.

Stop and ask the user only for architecture decisions, risky migrations,
possible data loss, major dependency changes, or security-critical ambiguity.
