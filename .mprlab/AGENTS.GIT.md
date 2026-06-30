# AGENTS.GIT.md

## Scope

Git and GitHub workflow guidance for this repository. Use this guide whenever branch, commit, pull request, release, or history operations are in scope.

## Rules

- The production branch is `master` unless the repo explicitly documents a different current branch.
- Use forward-only history. Do not rewrite, rebase, force-push, or amend published work.
- Branch names use taxonomy prefixes: `feature/`, `improvement/`, `bugfix/`, `maintenance/`, or `blocked/`.
- Keep one concrete issue or task per branch.
- Prefer repo-native commands and documented release helpers.
- Do not commit secrets, local env files, generated caches, or ephemeral planning files.
- Do not run deploy or publish commands unless the user explicitly asks for that operation and the repo contract allows the agent to do it.

## Pull Requests

- Open pull requests only after required local validation passes or a concrete blocker is documented.
- PR descriptions should summarize changed behavior and validation run.
- Keep release, publish, deploy, and production availability as separate statuses.

## Forbidden Operations

- `git push --force`
- `git rebase`
- `git reset --hard`
- history rewrites
- deleting or replacing user work without explicit instruction

## Validation

Before finalizing Git work, run the repo-native validation required by `AGENTS.md` and `.mprlab/POLICY.md`, then check:

```bash
git diff --check
git status --short
```
