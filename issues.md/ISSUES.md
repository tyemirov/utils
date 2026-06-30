# ISSUES (Append-only section-based log)

Entries record newly discovered requests or changes, with their outcomes. No instructive content lives here. Read @NOTES.md for the process to follow when fixing issues.

Read @AGENTS.md, @README.md and ARCHITECTURE.md. Read @POLICY.md, @NOTES.md, and @ISSUES.md under issues.md/folder. Start working on open issues. Prioritize bugfixes and maintenance. Work autonomously and stack up PRs. 

Each issue is formatted as `- [ ] [UT-<number>]`. When resolved it becomes `- [x] [UT-<number>]`.

## Features (100–199)

- [x] [UT-100] Add a shared strict runtime config package. (Move the reusable YAML config runtime contract needed by MediaOps and LoopAware into `utils`, so consumers can load one typed config file with declared environment placeholders instead of depending on ad hoc Cobra/Viper/env handling.)

Resolved: added `runtimeconfig` with typed loaders, default/explicit config path resolution, strict declared environment references, single-pass YAML expansion through `configfile`, known-field typed decode, required section loading, effective settings, and selected scalar value maps. Expanded `configfile` built-in value schemas for host:port, duration, positive integer, email, and hex-encoded 32-byte secrets, updated `cmd/configenvcheck` to reuse the exported schema catalog, and removed the legacy `preflight/viperconfig` adapter plus Viper module dependency. Changed files: `runtimeconfig/runtimeconfig.go`, `runtimeconfig/runtimeconfig_test.go`, `configfile/schemas.go`, `configfile/schemas_test.go`, `cmd/configenvcheck/main.go`, `cmd/configenvcheck/main_test.go`, `preflight/README.md`, `go.mod`, `go.sum`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `issues.md/ISSUES.md`. Verified with `timeout -k 350s -s SIGKILL 350s make test`, `timeout -k 350s -s SIGKILL 350s make lint`, `timeout -k 350s -s SIGKILL 350s make test-coverage`, and `timeout -k 350s -s SIGKILL 350s make ci`.

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

## Maintenance

### Recurring

- [ ] [M400R] (P2) Backlog hygiene and archive
  Goal:
  Keep the issue tracker reliable, readable, and focused on active work while preserving resolved history in the appropriate archive.

  Requirements:
  - Cadence: run weekly during active development and before each release cut.
  - Validate section names, identifier prefixes, recurrence suffixes, priority markers, dependencies, and duplicate IDs against the current `issues-md-format.md`.
  - Reconcile stale statuses, duplicate issues, broken references, obsolete instructions, and entries filed under the wrong section.
  - Move completed non-recurring history to the repository issue archive or durable documentation when the active tracker becomes noisy.
  - Keep active, blocked, planning, and recurring entries visible in `ISSUES.md`.

  Deliverables:
  - Normalized `ISSUES.md` structure and statuses.
  - Updated issue archive or docs when completed entries are removed from the active tracker.
  - A short `Last run:` note summarizing the cleanup and any follow-up issues filed.

  Validation:
  - Re-read `ISSUES.md` after edits and confirm every issue is under the right section with a unique section-aware ID.
  - Confirm recurring entries remain open and keep the `R` suffix.
  - Confirm no active, blocked, recurring, or planning work was archived.

- [ ] [M401R] (P2) Polish open issues
  Goal:
  Keep unresolved work executable by making each open issue concrete, ordered, and testable.

  Requirements:
  - Cadence: run weekly during active development and before handing a repo to automated execution.
  - Review every unresolved non-recurring issue for missing context, dependencies, repro steps, acceptance criteria, and validation expectations.
  - Make priorities concrete and ensure each open issue has actionable deliverables.
  - Merge duplicate open issues or add explicit dependency links when separate entries must remain.
  - Do not close or implement issues as part of this polish pass unless that work is separately requested.

  Deliverables:
  - Open issues with enough detail for a person or agent to execute without rediscovery.
  - New or updated dependency markers where ordering matters.
  - A short `Last run:` note listing the number of issues polished and any blockers found.

  Validation:
  - Sample the open entries after the pass and confirm each has clear next actions and validation expectations.
  - Confirm no recurring runbook was marked complete.
  - Confirm duplicates were merged or explicitly cross-referenced.

- [ ] [M402R] (P2) Architecture and policy review
  Goal:
  Catch architecture, policy, and workflow drift before it becomes hidden maintenance debt.

  Requirements:
  - Cadence: run monthly, before large refactors, and after major framework or runtime changes.
  - Review the codebase, docs, and workflow against `AGENTS.md`, `POLICY.md`, stack guides, and the current architecture notes.
  - Look for drift from forward-only contracts, edge-validation boundaries, smart-constructor usage, testing policy, and module ownership.
  - Record findings as new Maintenance issues with concrete scope, priority, and validation.
  - Close the pass with a no-action note only when the review finds no actionable drift.

  Deliverables:
  - New Maintenance issues for each actionable architecture or policy drift finding.
  - Updated notes on areas reviewed and areas intentionally left unchanged.
  - A short `Last run:` note with the review scope and outcome.

  Validation:
  - Confirm every finding is represented as an issue with owner-readable context and validation criteria.
  - Confirm no implementation changes were mixed into the review runbook unless separately requested.
  - Confirm all recurring runbooks remain open.

- [ ] [M403R] (P1) Dependency and security audit
  Goal:
  Keep third-party dependencies, runtime versions, and security-sensitive configuration within the current supported contract.

  Requirements:
  - Cadence: run weekly for active apps and before each release cut.
  - Inspect package managers, lockfiles, language toolchains, container bases, and generated clients for known vulnerabilities or stale direct dependencies.
  - Review auth, secret, CORS, CSP, SQL, network, and permission-sensitive configuration for drift from the current contract.
  - Prefer current supported dependencies; do not add compatibility shims for obsolete dependency behavior.
  - File separate Maintenance or BugFix issues for each actionable vulnerability, unsupported runtime, or security-contract gap.

  Deliverables:
  - Documented audit commands or data sources used for the pass.
  - Updated issues for each actionable dependency or security finding.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the repository-native audit, lint, or dependency checks used for the pass.
  - Confirm every finding is either filed, fixed under a separate issue, or explicitly marked not applicable with evidence.
  - Confirm no secrets or private payloads were written into the tracker.

- [ ] [M404R] (P1) CI, release, and artifact health
  Goal:
  Keep the repository's validation, release, publication, and generated artifact surfaces trustworthy.

  Requirements:
  - Cadence: run before every release, publish, or deploy, and weekly for critical services.
  - Verify repository-native CI, lint, format, coverage, release, publish, Docker image, Pages, and artifact workflows still match the documented contract.
  - Check generated artifacts, release tags, published images, and Pages outputs for source-to-public drift.
  - File concrete follow-up issues for failing gates, stale artifacts, missing release prerequisites, or undocumented workflow changes.
  - Do not perform production deployment from this runbook unless the operator explicitly requests that deployment.

  Deliverables:
  - Recorded gate status and artifact surfaces inspected.
  - Follow-up issues for each reproducible CI, release, publish, or artifact drift problem.
  - A short `Last run:` note with commands run and any skipped surfaces.

  Validation:
  - Use repository-native `make` targets or documented release helpers for checks.
  - Confirm release and deployment ownership boundaries remain separate.
  - Confirm public or published artifacts match the intended source revision when that surface is inspected.

- [ ] [M405R] (P1) Code contract and static hygiene
  Goal:
  Keep source contracts explicit, current, and statically guarded against policy drift.

  Requirements:
  - Cadence: run monthly and before large refactors.
  - Scan for dead code, unused exports, duplicated literals, silent fallbacks, legacy aliases, compatibility reads, and zero-but-invalid domain states.
  - Check static analysis, coverage, schema, and contract guards that are supposed to prevent drift.
  - File focused Maintenance issues for each concrete violation instead of broad cleanup placeholders.
  - Keep the current canonical contract only; do not preserve obsolete behavior unless a product requirement explicitly says so.

  Deliverables:
  - Issue entries for each actionable static hygiene or contract violation.
  - Notes on static tools, searches, and contract guards used during the pass.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the relevant static checks, contract tests, or repository searches used to identify drift.
  - Confirm every finding has a narrow follow-up issue and does not duplicate existing backlog work.
  - Confirm no implementation changes were mixed into the audit unless separately requested.

- [ ] [M406R] (P1) Production drift and health
  Goal:
  Detect when production, public, or scheduled runtime state has drifted from the intended repository contract.

  Requirements:
  - Cadence: run weekly for deployed services and after each publish or deploy.
  - Compare current source, runtime configuration, published images, public routes, scheduled jobs, and health checks for drift.
  - Inspect real operator-facing surfaces rather than assuming merged source is deployed.
  - File follow-up issues for stale images, stale Pages output, missing routes, failed monitors, invalid production config, or undocumented runtime differences.
  - Stop before production deploy or destructive operator actions unless the operator explicitly requests them.

  Deliverables:
  - Recorded source revision, public artifact, route, image, or health surfaces inspected.
  - Follow-up issues for each source-to-runtime drift finding.
  - A short `Last run:` note with evidence links or commands used.

  Validation:
  - Verify inspected production or public surfaces directly where access is available.
  - Confirm any deploy-required finding is filed with the exact publish/deploy boundary and owner.
  - Confirm no production state was changed by the audit unless explicitly requested.

- [ ] [M407R] (P2) Documentation and runbook hygiene
  Goal:
  Keep durable documentation and runbooks aligned with the current behavior users and operators actually rely on.

  Requirements:
  - Cadence: run before release cuts and after merge bursts that change user-facing or operator-facing behavior.
  - Review README, ARCHITECTURE, PRD, CHANGELOG, docs, runbooks, setup guides, and local workflow notes for stale behavior or missing new contracts.
  - Update docs when closed issues changed durable behavior, public APIs, operator workflows, release semantics, or deployment expectations.
  - Remove or rewrite stale instructions instead of preserving obsolete alternatives.
  - File separate issues for documentation gaps that require product or implementation decisions.

  Deliverables:
  - Updated documentation or filed follow-up issues for each gap.
  - A short `Last run:` note listing docs inspected and changes made.
  - Cross-references from archived issue history to durable docs when useful.

  Validation:
  - Check links, command names, paths, and public contract descriptions touched by the pass.
  - Confirm docs describe the current canonical path only.
  - Confirm issue archive and active tracker references remain consistent.

- [x] [UT-410] Extract a shared browser transport runtime beneath jseval. (Add `browsertransport` with transport profiles, reusable sessions, SOCKS forwarding, HTTP client helpers, and one-shot rendering; make `jseval` a compatibility wrapper with migrated coverage.)

`jseval` had grown into the real browser runtime while downstream repos needed
the underlying transport model directly. Extract the shared proxy-aware browser
and HTTP scaffolding into a dedicated package so projects can reuse sessions and
transport profiles without copying renderer internals.

- [x] [UT-407] Add Go CI gates (fmt/vet/staticcheck/ineffassign) and fix baseline failures. (Update GitHub Actions; ignore PLAN.md; normalize -0 formatting; export pointer helpers.)
- [x] [UT-408] Add missing ARCHITECTURE.md. (Document package layout, design principles, and tooling.)
- [x] [UT-409] Add preflight config reporting helpers and Viper adapter for shared service tooling. (Imported preflight package from TAuth and wired Viper adapter + docs.)
