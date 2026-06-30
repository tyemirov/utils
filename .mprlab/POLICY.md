# Confident Programming

This policy is binding for agents working in this repository.

## Operator Rules

- Validate only at edges: I/O, HTTP, CLI, DB adapters, browser bootstrap, imported files, and other external boundaries.
- Make illegal states unrepresentable with domain types, smart constructors, dataclasses, enums, or closed action objects.
- Fail fast on impossible states.
- Wrap boundary errors with operation and subject context.
- Keep core modules free of duplicated validation after an object has been validated.
- Keep interfaces narrow. Prefer domain types over loose strings, maps, booleans, or `any` values when a domain type exists.
- Centralize reusable literals: paths, operation names, event names, config keys, status values, and shared messages.
- Tests target public contracts and invariants, not defensive branches.
- Prefer black-box integration and end-to-end tests through real entry points.

## Prohibited Patterns

- Silent fallbacks, best-effort behavior, legacy aliases, and compatibility reads unless an explicit product requirement says the behavior is current.
- Duplicated validation inside core modules.
- Exporting invalid zero-values as usable domain objects.
- Swallowing errors.
- Increasing waits or timeouts as the primary fix for flakiness.
- Boolean parameters that switch unrelated behaviors.
- Hardcoded workflow, path, event, or message literals when a canonical constant or backend payload exists.
- Unit tests as a substitute for public contract coverage.

## Validation

- Use repository-native `make` targets when available.
- Prefer `make ci` for full validation.
- Use focused `make test`, `make lint`, or documented stack commands for narrow investigations.
- For frontend behavior, verify through a browser test when the behavior is user-visible.
- For services and CLIs, verify through HTTP, CLI, or public API entry points.

## Language Rules

### Go

- Use smart constructors returning `(Type, error)` when a type has invariants.
- Do not export invalid zero-values.
- Wrap errors with `%w`.
- Prefer integration tests through real HTTP, CLI, or package entry points.
- `make lint` must include `go vet`, `staticcheck`, and `ineffassign` when those tools are part of the repo contract.

### Python

- Use `@dataclass(frozen=True)` or Pydantic when already in use.
- Validate in constructors or edge adapters.
- Use type hints throughout.
- Prefer pytest scenarios through public entry points.
- Unit tests are allowed only as narrow guardrails for pure deterministic helpers.

### JavaScript And Frontend

- Put `// @ts-check` at the top of new or edited JavaScript modules when the repo uses checked JS.
- Use JSDoc typedefs for domain objects and payload contracts.
- Components render validated state and emit intent.
- Backend clients own request construction and response validation.
- User-visible behavior belongs in browser or integration coverage.

## Self-Check

Before claiming completion:

- External inputs are validated once at the edge.
- Core modules consume validated domain values.
- Error paths include operation and subject context.
- Reusable literals are centralized.
- Public behavior is covered through public entry points.
- Repo-native validation was run or a concrete blocker is documented.
