# ISSUES (Append-only section-based log)

Entries record newly discovered requests or changes, with their outcomes. No instructive content lives here. Read @NOTES.md for the process to follow when fixing issues.

Read @AGENTS.md, @README.md and ARCHITECTURE.md. Read @POLICY.md, @NOTES.md, and @ISSUES.md under issues.md/folder. Start working on open issues. Prioritize bugfixes and maintenance. Work autonomously and stack up PRs. 

Each issue is formatted as `- [ ] [UT-<number>]`. When resolved it becomes `- [x] [UT-<number>]`.

## Features (100–199)

## Improvements (200–299)

## BugFixes (300–399)

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
