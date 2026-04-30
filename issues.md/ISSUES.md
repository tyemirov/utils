# ISSUES (Append-only section-based log)

Entries record newly discovered requests or changes, with their outcomes. No instructive content lives here. Read @NOTES.md for the process to follow when fixing issues.

Read @AGENTS.md, @README.md and ARCHITECTURE.md. Read @POLICY.md, @NOTES.md, and @ISSUES.md under issues.md/folder. Start working on open issues. Prioritize bugfixes and maintenance. Work autonomously and stack up PRs. 

Each issue is formatted as `- [ ] [UT-<number>]`. When resolved it becomes `- [x] [UT-<number>]`.

## Features (100–199)

## Improvements (200–299)

- [x] [UT-200] Add lease-capable proxy rotation selector. (Move the reusable provider/user lease and reservation behavior needed by browser/manual crawlers into `crawler` while preserving the existing proxy rotation selector API.)

Resolved: added `ProxyLease`, `ProxyLeaseSelector`, required/acquire/report/release helpers, request-context attachment, reservation tracking for concurrent manual/browser leases, and compatibility delegation for the existing `ProxyRotationSelector` API. The shared selector keeps successful leases sticky and uses immediate provider rotation on failure while advancing the failed provider's next user for the next return. Added crawler tests for the new lease contract, stale generation handling, duplicate proxy URLs, invalid/empty configs, flat-list style providers, request context metadata, release/reuse, and compatibility branches. Changed files: `crawler/proxy_rotation_selector.go`, `crawler/proxy_lease_selector_test.go`, `crawler/proxy_rotation_selector_test.go`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, and `timeout -k 350s -s SIGKILL 350s make ci`.

## BugFixes (300–399)

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
