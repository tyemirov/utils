# Architecture

`tyemirov/utils` is a small collection of reusable Go packages. The repository is
organized by package (not by application), so downstream projects can import
only the helpers they need.

## Packages

- `browsertransport`: Shared proxy-aware browser and HTTP transport runtime for
  scraping workloads, including browser profiles, reusable sessions, SOCKS
  forwarding, and one-shot render helpers.
- `file`: Filesystem helpers (delete, close, read/write convenience).
- `jseval`: Compatibility wrapper around `browsertransport` for one-shot page
  rendering.
- `llm`: OpenAI-compatible chat client (`Client`) plus a retry/backoff wrapper
  (`Factory`).
- `math`: Small numeric helpers (`Min`, `Max`, `FormatNumber`, `ChanceOf`).
- `pointers`: Pointer helpers for primitive values.
- `scheduler`: Retry-aware periodic worker with exponential backoff and a
  persistence interface for attempts.
- `system`: Environment variable helpers.
- `text`: String normalization helpers.
- `test`: Black-box tests that exercise package behavior via public APIs.

## Design Principles

- Packages are intentionally small, with a minimal public API surface.
- Side effects (network/time) are injected where needed (for example, HTTP
  client and sleep function injection in `llm`).
- Validation is expected at boundaries; core helpers assume valid inputs unless
  documented otherwise.

## Browser Rendering Stack

- `browsertransport` owns the reusable runtime for proxy-aware scraping:
  browser transport profiles, long-lived browser sessions, short-lived render
  tabs, SOCKS forwarding, proxy-auth wiring, one-shot page rendering, and HTTP
  client construction.
- `jseval` stays as a compatibility layer so existing downstream callers can
  keep using `RenderPage` and `RenderPages` without depending on the richer
  transport API directly.

## LLM Module (`llm`)

- `Client` is the low-level HTTP client. It:
  - Builds the JSON payload.
  - Applies a request timeout via `context.WithTimeout`.
  - Reads and parses the response body, returning a trimmed string result.
- `Factory` wraps a `Client` and adds retry/backoff behavior, using a pluggable
  `SleepFunc` to keep retry timing testable.

## Tooling & CI

- Local: `gofmt`, `go vet`, `staticcheck`, `ineffassign`, and `go test ./...`.
- CI mirrors the same checks via GitHub Actions.
