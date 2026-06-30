# Architecture

`tyemirov/utils` is a small collection of reusable Go packages. The repository is
organized by package (not by application), so downstream projects can import
only the helpers they need.

## Packages

- `browsertransport`: Shared proxy-aware browser and HTTP transport runtime for
  scraping workloads, including browser profiles, reusable sessions, SOCKS
  forwarding, and one-shot render helpers.
- `configfile`: Strict YAML config loading with scalar-only environment
  interpolation, explicit missing-variable errors, no default-substitution
  syntax, single-document stream validation, known-field decoding, and
  registry-backed required/optional environment validation with optional value
  schemas. `cmd/configenvcheck` exposes the same contract to deployment
  preflights without requiring each caller to write a Go adapter first.
- `crawler`: Shared crawling helpers, including provider/user-aware proxy lease
  selection and operation-scoped failed-lease tracking for scrape batches.
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
  client construction. Direct HTTP transport profiles bypass ambient
  `HTTP_PROXY` and `HTTPS_PROXY` environment variables; callers must choose an
  explicit HTTP or SOCKS profile when a proxy is required.
- `jseval` stays as a compatibility layer so existing downstream callers can
  keep using `RenderPage` and `RenderPages` without depending on the richer
  transport API directly.

## Crawler Proxy Rotation

- `ProxyLeaseSelector` owns global provider/user rotation state. It keeps a
  successful lease sticky until failure, then advances to the next provider
  immediately while advancing the failed provider's next user for the next
  return. When all healthy leases are already reserved by in-flight requests, it
  reuses the least-reserved healthy lease instead of treating reservations as
  candidate exhaustion. Neutral terminal crawler responses release their lease
  without recording proxy success or failure, so reservations only reflect
  active in-flight requests. Stale-generation successes still clear proxy
  health and release their reservation, but they do not rewind provider/user
  cursors that were advanced by a concurrent failure.
- `ProxyLeaseAttemptScope` is caller-created for one scrape or request batch. It
  remembers which leases failed during that operation, skips those leases on
  later acquisitions, and returns `ErrProxyLeaseCandidatesExhausted` once every
  configured candidate has failed.
- `RetryPolicyRotateProxy` uses a rotation-only proxy report by default so
  content-level retry decisions rotate away from the current lease without
  recording proxy health failure. Platform hooks opt into critical cooldown with
  `RetryDecision.ProxyFailureSeverity` only when the proxy itself is unhealthy.
  `RetryDecision.ProxyFailureKind` and proxy failure diagnostics keep challenge,
  status, transport, provider-auth, and provider-account reasons structured so
  shared selector pools can avoid health cooldowns for content challenges and
  explain candidate exhaustion with reason buckets. Provider credential
  failures such as HTTP 402, HTTP 407, `Payment Required`, and
  `Proxy Authentication Required` immediately quarantine the affected lease and
  retry only alternate proxy candidates; ordinary status-0 transport failures
  remain on the transient retry path.

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
