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

- Use test-driven development with an inverted test pyramid.
- We **strive for 100% test coverage**, achieved primarily through integration/black-box suites whose scenarios are exhaustive enough to exercise all meaningful branches and error paths.
- For CLI and backend services, tests compile or run the real program/CLI entrypoints, capture exit codes and output (stdout/stderr, files, side effects), and assert against those observable results—not internal functions.
- For web/UI, tests run the app and backing web server, drive flows through the browser, and assert against the rendered page, DOM state, events, and other user-visible behavior.
- Use focused unit tests for complex algorithms, calculations, and isolated logic when useful.
- Require integration coverage of public behavior for product acceptance.

## Tech Stack Guides

Stack-specific instructions now live in dedicated files. Apply the relevant guide alongside the shared policies above.

- Backend (Go): `.mprlab/AGENTS.GO.md`
- Git and version control workflow: `.mprlab/AGENTS.GIT.md`

<!-- BEGIN MPRLAB-GOVERNANCE -->
## MPR Lab Governance

Root `AGENTS.md` is the agent entrypoint. Shared rules live under `.mprlab/`.

Read `.mprlab/POLICY.md` for every task.
Read the following files only when their condition applies.
Read each selected guide in full before its first applicable action.

- Before edits: `.mprlab/PLANNING.md`.
- For technical prose: `.mprlab/AGENTS.DOCS.md` and `.mprlab/TERMINOLOGY.md`.
- For issue work: the selected issue and its dependencies in `.mprlab/ISSUES.md`.
- For tracker edits: `.mprlab/issues-md-format.md`.
- For Git operations: `.mprlab/AGENTS.GIT.md`.
- For Go changes: `.mprlab/AGENTS.GO.md`.
- For Python changes: `.mprlab/AGENTS.PY.md`.

File permission modes are outside agent scope.
Never examine, validate, compare, require, change, or record a file permission mode.
Never use a file permission mode in acceptance, security, credential, execution, publication, deployment, or failure analysis.
The values `0600` and `7777` have no governance meaning.
This rule does not change service authorization or operation authority.

Do not create `.mprlab/AGENTS.md`. Scoped guidance belongs in `.mprlab/AGENTS.*.md` files.
If guidance conflicts, obey `.mprlab/POLICY.md` first, then root `AGENTS.md`, then the applicable scoped guide.
<!-- END MPRLAB-GOVERNANCE -->
