# ISSUES.md Format

This document describes the canonical ISSUES.md layout and section-aware identifier scheme.

## Structure

- The file starts with a title line, for example `# ISSUES`.
- Issues are grouped under level-2 headings.
- Sections are `BugFixes`, `Improvements`, `Maintenance`, `Features`, and `Planning`.
- Optional subheadings may organize a section, but issue IDs must still match the parent section.

## Issue Entries

Each issue entry is a single list item:

```text
- [ ] [B042] (P1) {I007} Short title
```

Rules:

- `[ ]` means open.
- `[-]` means taken.
- `[!]` means blocked and must include a `Blocked:` body line.
- `[x]` means closed.
- The external ID is required.
- Priority `(P0)` through `(P2)` is optional.
- Dependencies `{ID,ID}` are optional.
- The title is required.

## Identifiers

Format: `<SectionLetter><SequenceNumber>[R]`.

Section letters:

- `B` = BugFixes
- `I` = Improvements
- `M` = Maintenance
- `F` = Features
- `P` = Planning

Numbers increment independently per section and use three digits. A capital `R` suffix marks a recurring issue, for example `[M400R]`. A separate `R` token after the identifier is invalid.

Recurring entries represent standing or repeated work. Scheduling, timers, and job IDs are outside the ISSUES.md format.

Legacy repo-prefixed identifiers are invalid.

## Body Text

Additional body lines are indented by two spaces. Structured issue bodies should use plain labels:

- `Goal:`
- `Requirements:`
- `Deliverables:`
- `Validation:`
- `Blocked:`

`Blocked:` is required only for blocked issues and must name the external dependency, missing input, or policy decision preventing progress.
