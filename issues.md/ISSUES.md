# ISSUES (Append-only section-based log)

Entries record newly discovered requests or changes, with their outcomes. No instructive content lives here. Read @NOTES.md for the process to follow when fixing issues.

Read @AGENTS.md, @README.md and ARCHITECTURE.md. Read @POLICY.md, @NOTES.md, and @ISSUES.md under issues.md/folder. Start working on open issues. Prioritize bugfixes and maintenance. Work autonomously and stack up PRs. 

Each issue is formatted as `- [ ] [UT-<number>]`. When resolved it becomes `- [x] [UT-<number>]`.

## Features (100–199)

## Improvements (200–299)

- [x] [UT-201] Add operation-scoped proxy lease exhaustion tracking. (Move per-operation failed-lease memory into `crawler` so downstream scrapers can skip candidates that already failed during the same scrape/request batch without changing global sticky rotation behavior.)

Resolved: added `ProxyLeaseAttemptScope`, `ErrProxyLeaseCandidatesExhausted`, `ProxyLeaseCandidatesExhaustedError`, and `ProxyLeaseSelector.CandidateCount` to support operation-scoped failed-lease skipping without changing global selector stickiness. Added crawler coverage for candidate skipping, exhausted errors, nil selectors/scopes, invalid leases, and zero-value scope initialization; documented the new crawler API in README and architecture notes; updated CHANGELOG for v0.15.1. Changed files: `crawler/proxy_rotation_selector.go`, `crawler/proxy_lease_selector_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test` and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-200] Add lease-capable proxy rotation selector. (Move the reusable provider/user lease and reservation behavior needed by browser/manual crawlers into `crawler` while preserving the existing proxy rotation selector API.)

Resolved: added `ProxyLease`, `ProxyLeaseSelector`, required/acquire/report/release helpers, request-context attachment, reservation tracking for concurrent manual/browser leases, and compatibility delegation for the existing `ProxyRotationSelector` API. The shared selector keeps successful leases sticky and uses immediate provider rotation on failure while advancing the failed provider's next user for the next return. Added crawler tests for the new lease contract, stale generation handling, duplicate proxy URLs, invalid/empty configs, flat-list style providers, request context metadata, release/reuse, and compatibility branches. Changed files: `crawler/proxy_rotation_selector.go`, `crawler/proxy_lease_selector_test.go`, `crawler/proxy_rotation_selector_test.go`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

## BugFixes (300–399)

- [x] [UT-310] Keep normal rotate-proxy retries from cooling down the full proxy pool. (Record normal proxy failures for content-level rotate decisions such as CAPTCHA or wrong delivery context, while preserving an explicit critical cooldown option for genuinely unhealthy proxy candidates.)

Resolved: `RetryPolicyRotateProxy` decisions now record normal proxy failures by default, keeping per-product rotation immediate without forcing every candidate into global cooldown. Added `RetryDecision.ProxyFailureSeverity` for callers that need explicit critical cooldown, covered the normal tracker path, lease-reporter path, and critical opt-in path, and documented the retry semantics. Changed files: `crawler/platform.go`, `crawler/response.go`, `crawler/response_test.go`, `crawler/coverage_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with pre-change and post-change `timeout -k 350s -s SIGKILL 350s make ci`; focused `timeout -k 180s -s SIGKILL 180s go test -failfast ./crawler -count=1` also passed.

Review follow-up: normal `RetryPolicyRotateProxy` decisions now use a rotation-only lease report instead of incrementing proxy health failure counters; critical cooldown remains explicit through `RetryDecision.ProxyFailureSeverityCritical`. Added regression coverage for repeated normal rotations staying available under the circuit breaker.

- [x] [UT-311] Fix stale-generation proxy successes so they clear proxy health after concurrent failures. (A successful response from an older lease generation currently releases the reservation but returns before clearing proxy health, while stale-generation failures still count against proxy health.)

Under high concurrency, one request can fail and advance selector generation while another in-flight request using the same earlier generation later succeeds. `ProxyLeaseSelector.ReportSuccess` currently checks `lease.Generation != selector.generation` before `recordSuccessLocked`, so the valid success cannot reset failure counters or cooldown state. This makes proxy health pessimistic and can leave candidates cooled even after a real successful response proves they work. Move health recovery ahead of the generation guard while preserving the guard for sticky provider/user cursor updates. Add a regression where a stale-generation success clears health without moving `activeProvider` or `nextUser`, and verify reservations are still released exactly once.

Resolved: `ProxyLeaseSelector.ReportSuccess` now records proxy health recovery after a known lease is released and before stale-generation cursor checks, so concurrent successes clear cooldown without rewinding provider/user rotation state. Added regression coverage for duplicate in-flight leases where a critical failure advances generation and the stale success clears health, preserves `activeProvider`/`nextUser`, and releases reservations idempotently. Changed files: `crawler/proxy_rotation_selector.go`, `crawler/proxy_lease_selector_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-312] Add guardrails and diagnostics for critical proxy failures in shared selector pools. (A caller-side content/challenge misclassification can currently mark many leases as critical, immediately cool candidates, and exhaust a shared pool.)

`RetryProxyFailureSeverityCritical` intentionally cools a proxy immediately, but the crawler layer does not record enough structured reason data to distinguish confirmed transport failure from content-level challenge rotation or caller misclassification. PoodleScanner's Amazon scans showed a burst of `reason=CAPTCHA` decisions turning into immediate 30s cooldowns and later `crawler: proxy lease candidates exhausted` errors. Preserve the explicit critical path for confirmed proxy-unhealthy cases, but add structured failure reason/kind diagnostics and a non-health-penalizing content/challenge rotate path so caller bugs cannot silently poison the whole pool. Candidate-exhaustion diagnostics should report why candidates were unavailable, including challenge, status 0, 502/503/504, and provider auth/account classes.

Resolved: added `ProxyFailureKind`, `ProxyFailureDiagnostic`, diagnostic retry-decision fields, diagnostic-aware lease reporter methods, and exhausted-candidate reason buckets. Content/challenge retry decisions now use a non-health-penalizing rotation path even if a caller marks them critical, while explicit critical transport/status/provider failures still cool candidates. `ProxyLeaseAttemptScope` can retain per-candidate diagnostics and candidate exhaustion errors now include buckets such as `challenge`, `status_0`, `status_502`, `status_503`, `status_504`, `provider_auth`, and `provider_account`. Changed files: `crawler/proxy_failure_diagnostic.go`, `crawler/platform.go`, `crawler/proxy_health.go`, `crawler/proxy_rotation_selector.go`, `crawler/response.go`, `crawler/service.go`, `crawler/proxy_lease_selector_test.go`, `crawler/response_test.go`, `crawler/coverage_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-313] Classify proxy auth and account failures separately from generic status-0 transport failures. (Provider/account errors such as `Payment Required` are currently treated as transient status-0 proxy failures and retried like ordinary network noise.)

`handleCollectorError` calls `recordProxyFailure(tracker, resp)` before the error is classified, and `recordProxyFailure` only sees response status. Provider account/auth failures often surface as status 0 transport errors, so strings such as `Payment Required` from IPROYAL collapse into the same path as generic connection failures. Pass the error into proxy failure classification, detect provider-account/auth cases such as HTTP 402, HTTP 407, `Payment Required`, and `Proxy Authentication Required`, and quarantine or fail fast for the affected provider/credential instead of burning the full retry budget for every product URL. Keep ordinary timeouts and transient status 0 failures on the transient retry path.

Resolved: collector errors now classify the response status and transport error before recording proxy health. Provider account/auth failures, including status 0 errors containing `Payment Required`, HTTP 402, HTTP 407, and `Proxy Authentication Required`, are recorded as critical diagnostic lease failures so the affected credential is quarantined immediately. Credential failures retry only alternate proxy candidates with no delay and fail fast when no alternate candidate exists; ordinary status-0 transport errors still use the transient retry path. Changed files: `crawler/proxy_failure_diagnostic.go`, `crawler/proxy_health.go`, `crawler/service.go`, `crawler/service_integration_test.go`, `crawler/coverage_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-314] Make `httptransport` direct profiles bypass environment proxies. (`InferProfile("")` returns direct, but `NewClient` keeps `http.ProxyFromEnvironment`, so direct clients can still route through `HTTP_PROXY` or `HTTPS_PROXY`.)

`httptransport.NewClient` initializes every transport with `Proxy: http.ProxyFromEnvironment` and only overrides it when a concrete proxy URL is configured. A profile identified as `direct` should not use ambient environment proxies. Change direct mode to use `Proxy: nil`; keep explicit HTTP proxy URLs on `http.ProxyURL`; keep SOCKS profiles on the SOCKS `DialContext` path; and add tests with `HTTP_PROXY`/`HTTPS_PROXY` set that prove direct, explicit HTTP proxy, and SOCKS modes stay distinct. If environment proxy behavior is still needed, introduce it as an explicit profile option rather than the default direct behavior.

Resolved: `httptransport.NewClient` now leaves `http.Transport.Proxy` nil for direct profiles, so ambient `HTTP_PROXY`/`HTTPS_PROXY` settings are ignored unless a caller chooses an explicit proxy profile. Explicit HTTP proxy profiles still use `http.ProxyURL`, SOCKS profiles still use the SOCKS dialer path with no HTTP proxy function, and the browsertransport wrapper test now follows the same direct-mode contract. Changed files: `httptransport/httptransport.go`, `httptransport/httptransport_test.go`, `browsertransport/http_client_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-309] Release crawler proxy leases on neutral terminal responses. (Ensure response paths that finish without retrying or evaluating HTML, such as binary handlers, page-not-found titles, no-title exhaustion, and incomplete-content exhaustion, release the reserved proxy lease instead of leaving stale reservations behind.)

Resolved: response processing now releases tracked proxy leases for neutral terminal response paths without reporting proxy success or failure. Added crawler regression coverage for binary short-circuit handlers, page-not-found titles, missing-title exhaustion, incomplete-content exhaustion, and default retry-policy exhaustion; updated crawler docs and changelog. Changed files: `crawler/response.go`, `crawler/response_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-306] Build SOCKS dial targets with `net.JoinHostPort`. (Use bracket-aware host:port assembly in the SOCKS forwarder and add IPv6 dial-target regression coverage.)

The forwarder assembled upstream dial targets with raw string formatting, which
breaks IPv6 literals by producing invalid `host:port` strings like
`2001:db8::1:443`. CONNECT requests for IPv6 destinations therefore failed even
though the proxy handshake itself succeeded.

- [x] [UT-305] Honor caller cancellation during browser tab initialization. (Move the per-call timeout and caller-cancellation bridge ahead of `chromedpRunner(tabCtx)` in `browsertransport.WithTab`; add regression coverage for a stuck tab-init path.)

`WithTab` initialized the derived tab on the long-lived browser context before it installed the per-call timeout and caller-cancellation bridge. When the session parent was non-cancelable, a stuck tab init could ignore request cancellation and hang indefinitely.

- [x] [UT-304] Attach jseval HTTP proxy auth to the render target. (Create a dedicated render tab before proxy auth/fetch setup; add regression coverage for render-target binding and target initialization failures.)

`jseval.RenderPage` was enabling proxy auth on the parent browser context and then rendering on a derived context. That works only as long as both operations share the same CDP target; callers that introduce a dedicated render tab can lose the auth handler and fail with proxy-auth page load errors.

- [x] [UT-303] Skip dispatch when claim for attempt is lost. (Add optional claim hook in scheduler worker; skip dispatch/update when claim returns false or errors; add regression tests.)

When multiple workers contend for the same pending entry, the scheduler can dispatch duplicate attempts unless the repository can atomically claim ownership before side effects run. Add a claim gate so workers skip dispatch when claim returns false.

- [x] [UT-300] Close response body when transport returns both response and error. (Close response body on Do error; add regression test.)

The error path after httpClient.Do returns immediately without closing the response body when Do returns both a response and an error. Per net/http this can happen for protocol errors or cancellations, and the caller must still close Response.Body; skipping it leaks the underlying connection and prevents keep-alives. Consider closing httpResponse.Body before returning the error.

- [x] [UT-301] Guard nil contexts in factory Chat. (Treat nil contexts as background context; add regression test.)

Unlike Client.Chat, Factory.Chat assumes the caller passes a non-nil context and dereferences ctx.Err() unconditionally. Passing nil—which the client explicitly supports by falling back to context.Background()—will panic here, breaking callers that swap in a factory without changing their context handling.

- [x] [UT-302]  Validate response format schema before sending request (Fail fast when schema is missing or not a JSON object; add regression test.)

Chat marshals the request payload directly with json.Marshal even when ResponseFormat.Schema contains malformed JSON; json.RawMessage does not validate its contents, so json.Marshal succeeds and the client proceeds to POST an invalid body, returning a transport/HTTP error instead of failing fast with an encoding error (e.g., the schema used in TestClientChatFailsWhenMarshallingRequestPayload). This lets malformed response formats slip through and results in requests the API will reject.

- [x] [UT-307] Allow lease reuse when all proxies are currently reserved. (Return the least-reserved healthy proxy instead of exhausting candidates when concurrent in-flight requests exceed the configured proxy count.)

Returning an empty lease when every configured proxy is reserved makes `acquire()` emit `ErrProxyLeaseCandidatesExhausted`, which bubbles through `Select` into HTTP transport proxy callbacks and fails requests even though healthy proxies exist.

Resolved: `ProxyLeaseSelector` now falls back to the least-reserved healthy lease after all unreserved candidates are occupied, while cooldown-unavailable candidates remain skipped. Added regression coverage for saturated public `Select` behavior and updated lease-selector expectations/docs/changelog. Changed files: `crawler/proxy_rotation_selector.go`, `crawler/proxy_lease_selector_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

- [x] [UT-308] Reject trailing YAML documents after strict decode. (Require strict config loads to fail when a YAML stream contains more than one document instead of silently accepting only the first document.)

The strict config loader performs one YAML decode and returns success without requiring the next decode to be `io.EOF`, so later YAML documents can be ignored while operators believe those settings were applied.

Resolved: `configfile` now decodes exactly one YAML document before interpolation and requires EOF after the strict `KnownFields(true)` decode. Added `LoadYAMLBytes` and interpolation regressions for trailing documents, malformed trailing documents, and the final strict-decode EOF check; updated configfile docs/changelog. Changed files: `configfile/configfile.go`, `configfile/configfile_test.go`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

## Maintenance (407–449)

- [x] [UT-410] Extract a shared browser transport runtime beneath jseval. (Add `browsertransport` with transport profiles, reusable sessions, SOCKS forwarding, HTTP client helpers, and one-shot rendering; make `jseval` a compatibility wrapper with migrated coverage.)

`jseval` had grown into the real browser runtime while downstream repos needed
the underlying transport model directly. Extract the shared proxy-aware browser
and HTTP scaffolding into a dedicated package so projects can reuse sessions and
transport profiles without copying renderer internals.

- [x] [UT-407] Add Go CI gates (fmt/vet/staticcheck/ineffassign) and fix baseline failures. (Update GitHub Actions; ignore PLAN.md; normalize -0 formatting; export pointer helpers.)
- [x] [UT-408] Add missing ARCHITECTURE.md. (Document package layout, design principles, and tooling.)
- [x] [UT-409] Add preflight config reporting helpers and Viper adapter for shared service tooling. (Imported preflight package from TAuth and wired Viper adapter + docs.)

## Planning (do not implement yet) (450–499)
