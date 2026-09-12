Run the complete MiniKV project in PROJECT RUNNER mode.

AGENTS.md is the primary workflow. First read:
- PROGRESS.md
- PROJECT_CONTEXT.md
- The relevant sections of docs/MASTER_BUILD_PROMPT.md

Do not print the full master prompt.

Your objective is to complete the entire MiniKV project, not only the
current phase. Continue across sessions using PROGRESS.md as the persistent
state machine.

At session start:

1. Inspect PROGRESS.md and determine the current phase.
2. Identify the next incomplete milestone and exact next action.
3. Inspect the repository, go.mod, scripts, git status, recent history,
   relevant code, and existing tests.
4. Never assume a phase is complete because its checkbox is marked.
5. Verify implementation and quality-gate evidence before advancing phases.

Phase order:

1. Foundation
2. Core Engine
3. Persistence
4. Interfaces
5. Quality and Benchmarks
6. Release

For the current phase:

- Continue the next incomplete milestone.
- Work only within the current phase and required dependencies.
- Reuse existing architecture and abstractions.
- Do not perform unrelated refactoring.
- Implement production-quality behavior, not mockups or placeholders.
- Add meaningful tests.
- Keep engine, persistence, api, and cli package boundaries clean.

After every meaningful milestone:

1. Run gofmt.
2. Run the applicable tests.
3. Run go vet ./...
4. Run go test -race ./...
5. Run benchmarks when relevant.
6. Fix ordinary localized errors.
7. Update PROGRESS.md with factual results.
8. Review git diff and git status.
9. Commit the clean milestone locally with a clear message.

A phase may advance only when its applicable quality gate is satisfied:

- Feature implemented
- Code formatted (gofmt)
- Unit tests pass
- Integration tests pass where applicable
- go vet ./... passes
- go test -race ./... passes
- Persistence behavior tested where applicable
- Errors handled properly
- Documentation updated
- PROGRESS.md updated
- Git status reviewed

When a phase passes its quality gate:

1. Mark its checkbox complete in PROGRESS.md.
2. Record verification evidence and limitations.
3. Set the next phase as current.
4. Continue automatically with the next phase if session capacity allows.

Never fabricate code inspection, test, race-test, vet, benchmark, build,
or commit results. If a command was not run, write "Not run". If a tool
does not exist, write "Not available".

Fix normal implementation errors automatically. Stop and ask me only for:

- Architecture-level decisions
- Risky data-format changes
- Major dependency changes
- Possible data loss
- Security-critical ambiguity
- Changes outside the product specification

If session context, time, or capacity is becoming low:

1. Stop starting new work.
2. Finish the current safe checkpoint.
3. Run applicable validation.
4. Update PROGRESS.md.
5. Write the exact next action.
6. Review the diff.
7. Commit clean changes locally.
8. Do not push.

When all phases are marked complete, perform the final acceptance pass from
section 19 of docs/MASTER_BUILD_PROMPT.md:

- Verify clean-clone build and test workflow.
- Verify HTTP SET/GET/DELETE and status endpoints.
- Verify TTL expiration.
- Verify WAL recovery after abrupt process termination.
- Verify corrupted WAL is reported, not silently deleted.
- Verify snapshots work.
- Run benchmarks and document real results in docs/benchmarks.md.
- Verify CI passes and Docker build works.
- Verify README and design documentation are complete.
- Do not claim completion without evidence.

Only after the final acceptance criteria are actually verified:

1. Update PROGRESS.md with final release status.
2. Perform a final release-preparation review.
3. Commit the final clean changes locally.
4. Report:

PROJECT COMPLETE

- Final acceptance:
- Test results:
- Race test results:
- Benchmark results:
- Documentation status:
- Known limitations:
- Final commit:

Do not push to any remote repository.