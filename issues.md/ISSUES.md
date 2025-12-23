# ISSUES (Append-only section-based log)

Entries record newly discovered requests or changes, with their outcomes. No instructive content lives here. Read @NOTES.md for the process to follow when fixing issues.

Read @AGENTS.md, @README.md and ARCHITECTURE.md. Read @POLICY.md, @NOTES.md, and @ISSUES.md under issues.md/folder. Start working on open issues. Prioritize bugfixes and maintenance. Work autonomously and stack up PRs. 

Each issue is formatted as `- [ ] [UT-<number>]`. When resolved it becomes `- [x] [UT-<number>]`.

## Features (100–199)

## Improvements (200–299)

## BugFixes (300–399)

- [x] [UT-300] Close response body when transport returns both response and error. (Close response body on Do error; add regression test.)

The error path after httpClient.Do returns immediately without closing the response body when Do returns both a response and an error. Per net/http this can happen for protocol errors or cancellations, and the caller must still close Response.Body; skipping it leaks the underlying connection and prevents keep-alives. Consider closing httpResponse.Body before returning the error.

- [x] [UT-301] Guard nil contexts in factory Chat. (Treat nil contexts as background context; add regression test.)

Unlike Client.Chat, Factory.Chat assumes the caller passes a non-nil context and dereferences ctx.Err() unconditionally. Passing nil—which the client explicitly supports by falling back to context.Background()—will panic here, breaking callers that swap in a factory without changing their context handling.

- [x] [UT-302]  Validate response format schema before sending request (Fail fast when schema is missing or not a JSON object; add regression test.)

Chat marshals the request payload directly with json.Marshal even when ResponseFormat.Schema contains malformed JSON; json.RawMessage does not validate its contents, so json.Marshal succeeds and the client proceeds to POST an invalid body, returning a transport/HTTP error instead of failing fast with an encoding error (e.g., the schema used in TestClientChatFailsWhenMarshallingRequestPayload). This lets malformed response formats slip through and results in requests the API will reject.

## Maintenance (407–449)

- [x] [UT-407] Add Go CI gates (fmt/vet/staticcheck/ineffassign) and fix baseline failures. (Update GitHub Actions; ignore PLAN.md; normalize -0 formatting; export pointer helpers.)
- [x] [UT-408] Add missing ARCHITECTURE.md. (Document package layout, design principles, and tooling.)
- [x] [UT-409] Add preflight config reporting helpers and Viper adapter for shared service tooling. (Imported preflight package from TAuth and wired Viper adapter + docs.)

## Planning (do not implement yet) (450–499)
