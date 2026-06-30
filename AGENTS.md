# AGENTS.md

## Forward-Only Contract Discipline

This repository follows a forward-only, confident programming paradigm. This is a binding agent contract: no fallbacks, no backward compatibility, no legacy support, and no compatibility shims. Do not spend design or implementation effort on backward compatibility considerations except for explicit one-off data migrations into the current canonical contract.

Repeat for emphasis because this rule is binding: no fallbacks, no backward compatibility, no legacy compatibility. Delete or reject obsolete code paths, stale schemas, deprecated config, and old persisted shapes instead of preserving them through compatibility layers, dual reads/writes, aliases, or best-effort recovery.

One-off data migrations are allowed only when they move existing persisted data into the current schema in a bounded operation. After migration, remove the bridge and keep only the current contract.

## tyemirov/utils

A collection of small Go helpers that can be shared between projects. See README.md for details

## Document Roles

- `.mprlab/POLICY.md`: Binding validation, error-handling, and confident-programming policy.
- `.mprlab/PLANNING.md`: Durable planning contract and active-plan expectations.
- `.mprlab/issues-md-format.md`: Canonical issue tracker format and section-aware identifier scheme.
- `.mprlab/ISSUES.md`: Active issue tracker for open, blocked, planning, and recurring work.
- `.mprlab/ISSUES.legacy.md`: Historical snapshot of the pre-`.mprlab` tracker. Do not use it for active work.

### Document Precedence

- `.mprlab/POLICY.md` defines binding validation, error-handling, and “confident programming” rules.
- `AGENTS.md` (this file) defines repo-wide workflow, testing philosophy, and agent behavior; stack-specific AGENTS.* guides refine these rules for each technology.
- `.mprlab/AGENTS.*.md` files never contradict `AGENTS.md` or `.mprlab/POLICY.md`; if guidance appears inconsistent, defer to `.mprlab/POLICY.md` first, then `AGENTS.md`, and treat the stack guide as a refinement.
- `.mprlab/PLANNING.md` is process guidance and must not introduce rules that conflict with `.mprlab/POLICY.md` or any `AGENTS*.md` files.

### Issue Status Terms

- Open (`[ ]`): Needs decision and/or implementation.
- Taken (`[-]`): In progress for one concrete change.
- Blocked (`[!]`): Requires an external dependency or policy decision.
- Closed (`[x]`): Completed and verified; no further action.

### Validation & Confidence Policy

All rules for validation, error handling, invariants, and “confident programming” (no defensive checks, edge-only validation, smart constructors, CI gates) are defined in `.mprlab/POLICY.md`. Treat that document as binding; this file does not restate them.

### Build & Test Commands

- Use the repository `Makefile` for local automation. Invoke `make test`, `make lint`, `make ci`, or other documented targets instead of running ad-hoc tool commands.
- `make test` runs the canonical test suite for the active stack.
- `make lint` enforces linting rules before code review.
- `make ci` mirrors the GitHub Actions workflow and should pass locally before opening a PR.

### Tooling Workflow (Tests, Lint, Format)

- For any change intended to land, agents MUST ensure that all required tooling for the relevant stack (tests, linters, and formatters as defined in `.mprlab/AGENTS*` and `.mprlab/POLICY.md`) passes cleanly on the branch before code is merged or released.
- `.mprlab/PLANNING.md` defines the durable workflow expectations for active plans, blockers, and completion; agents should treat those steps as given but do not need to restate or modify them.

### Testing Philosophy

- Testing follows an **inverted test pyramid**: most coverage comes from high-value black-box integration and end-to-end tests; unit tests are optional and exist only when they add clear implementation guardrails.
- We **strive for 100% test coverage**, achieved primarily through integration/black-box suites whose scenarios are exhaustive enough to exercise all meaningful branches and error paths.
- For CLI and backend services, tests compile or run the real program/CLI entrypoints, capture exit codes and output (stdout/stderr, files, side effects), and assert against those observable results—not internal functions.
- For web/UI, tests run the app and backing web server, drive flows through the browser, and assert against the rendered page, DOM state, events, and other user-visible behavior.
- Unit tests are acceptable as **implementation guardrails**, but they are not product-level acceptance criteria, must not be the primary mechanism for achieving coverage, and may be removed when equivalent or stronger integration coverage exists.

## Tech Stack Guides

Stack-specific instructions now live in dedicated files. Apply the relevant guide alongside the shared policies above.

- Backend (Go): `.mprlab/AGENTS.GO.md`
- Git and version control workflow: `.mprlab/AGENTS.GIT.md`

<!-- BEGIN MPRLAB-GOVERNANCE -->
## MPR Lab Governance

Most workflow context files live under `.mprlab/`. The root `AGENTS.md` remains the repository entrypoint for agents.

Read these files before editing:

- `.mprlab/POLICY.md`: binding validation and confident-programming rules.
- `.mprlab/PLANNING.md`: durable planning contract.
- `.mprlab/issues-md-format.md`: issue tracker format and recurring identifier rules.
- `.mprlab/ISSUES.md`: active issue tracker.
- `.mprlab/AGENTS.GIT.md`: Git and pull request workflow.
- `.mprlab/AGENTS.GO.md`: Go guidance.

Do not create `.mprlab/AGENTS.md`. Scoped stack guidance belongs in `.mprlab/AGENTS.*.md` files.
If guidance conflicts, follow `.mprlab/POLICY.md` first, then root `AGENTS.md`, then the relevant scoped stack guide.
<!-- END MPRLAB-GOVERNANCE -->
