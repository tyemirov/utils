# AGENTS.GO.md

## Scope

Backend guidance for Go code. Follow root `AGENTS.md` and `.mprlab/POLICY.md` for shared workflow and confident-programming rules.

## Core Principles

- Reuse existing code first.
- Favor data structures, registries, and cohesive types over branching logic.
- Inject external effects: I/O, network, time, randomness, and OS state.
- Keep core logic pure where practical.
- Accept domain types instead of loose primitives when invariants exist.
- Return errors and wrap them with context.
- Keep public API surface minimal.

## Code Style

- Use descriptive identifiers. No single-letter names except conventional tiny scopes where the repo already allows them.
- Lift repeated string literals into constants.
- Use GoDoc for exported identifiers.
- No panics in library code.
- Use structured logging when the repo has a logger.
- Propagate `context.Context` through effectful boundaries.

## Testing

- Prefer black-box integration tests through HTTP, CLI, or public package entry points.
- Use table-driven scenarios where they cover contract permutations.
- Use `t.TempDir()` for temporary filesystem work.
- Do not add unit tests as a substitute for public behavior coverage.

## Validation

Use repo-native targets:

```bash
make fmt
make lint
make test
make ci
```

When wired, `make lint` should include `go vet`, `staticcheck`, and `ineffassign`.
