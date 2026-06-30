# Planning

Use this file as the durable planning contract for the repository.

## Planning Rules

- Read `AGENTS.md`, `.mprlab/POLICY.md`, relevant `.mprlab/AGENTS.*.md` guides, and current issue context before editing.
- Plan one concrete change at a time.
- Keep plans forward-only: choose the current canonical contract instead of preserving legacy paths.
- Record blockers with exact missing input, failing command, or external dependency.
- Do not turn planning notes into implementation unless the user or active issue explicitly asks for implementation.

## Working Plan

Use `.mprlab/PLAN.md` for the active working plan when the repository workflow expects one. Keep it short, current, and untracked when the repo contract says it is ephemeral.

Suggested shape:

```text
- [ ] Read repo guidance and target issue.
- [ ] Inspect the current implementation and tests.
- [ ] Make the scoped change.
- [ ] Run focused validation.
- [ ] Update issue notes or docs.
```

## Completion

A change is complete only when requested edits are done, required issue or documentation notes are updated, and validation has passed or a concrete blocker is documented.
