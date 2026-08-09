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
- [ ] Use the initial validation result for application changes.
- [ ] Make the scoped change.
- [ ] Run the smallest applicable target during the change.
- [ ] Complete the applicable validation after the last change.
- [ ] Update issue notes or docs.
```

## Completion

Complete a change only after you complete all requested edits and necessary documentation updates.

The applicable validation after the last change must pass. If validation cannot pass, record the concrete blocker.
