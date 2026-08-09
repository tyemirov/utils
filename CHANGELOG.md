# Changelog

## [Unreleased]

### Improvements
- Added strict local release, exact publication, and Go module proxy deployment targets.

## [v0.17.1] - 2026-08-09

- Merge pull request #40 from tyemirov/tyemirov/feature/F101-F102-runtimeconfig-integrations
- Merge remote-tracking branch 'origin/master' into tyemirov/feature/F101-F102-runtimeconfig-integrations
- test: cover repository-owned release lifecycle tooling
- feat(release): add immutable Go module release lifecycle
- docs: document release lifecycle commands
- feat(release): add Go module release lifecycle targets
- feat(release): add strict release and module proxy targets
- docs: expand repository guidance references
- docs: establish technical writing standards and agent guidance
- Merge pull request #39 from tyemirov/issues-md-1782933103668
- Update ISSUES.md

## [v0.17.0] - 2026-06-30

### Features ✨
- Added `runtimeconfig` package providing strict YAML runtime config loading with environment variable interpolation.
- Introduced `configenvcheck` utility for validating YAML environment requirements.
- Added support for single YAML document parsing and common EnvValueSchema implementations in `configfile`.

### Improvements ⚙️
- Centralized all environment schema validation into `configfile`.
- Added `fmt` Makefile target as an alias for `format`.
- Updated and clarified governance, agent policies, and architecture documentation.

### Removed
- Removed the legacy `preflight/viperconfig` adapter and Viper dependency path.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Added comprehensive tests for strict YAML loading, schema validation, and environment contract enforcement.
- New tests for `configenvcheck` CLI and YAML decode permutations.

### Docs 📚
- Documented new and updated governance, policy, and agent behavior under `.mprlab/`.
- Clarified runtime config loader and value handling in README and architecture docs.
- Provided new maintenance runbooks and requirements in issues tracker.
- Improved and migrated legacy issues and documentation structure.

## [v0.16.0] - 2026-05-29

### Features ✨
- Direct HTTP transport profiles bypass ambient `HTTP_PROXY` and `HTTPS_PROXY` environment variables.
- Provider credential failures (HTTP 402, 407, Payment Required, Proxy Authentication Required) quarantine affected leases and retry only alternate proxy candidates.
- Added structured proxy failure diagnostics with challenge, status, transport, provider-auth, and provider-account reasons for better proxy health management.

### Improvements ⚙️
- Proxy lease selector reuses least-reserved healthy lease under concurrency saturation.
- Released proxy leases on neutral terminal response paths to avoid stale reservations.
- Stale-generation successes clear proxy health without rewinding provider/user cursors after concurrent failures.
- Retry decisions distinguish normal rotate-proxy retries from critical proxy failures using `RetryDecision.ProxyFailureSeverity` and `RetryDecision.ProxyFailureKind`.
- Reject trailing YAML documents in config loads instead of silently ignoring them.
- Updated documentation to clarify proxy transport profiles and retry behavior.

### Bug Fixes 🐛
- Fixed direct HTTP transport proxy bypass issues.
- Fixed direct payment-required retry handling.
- Fixed proxy credential failures causing lease quarantine.
- Fixed critical proxy diagnostics and stale-generation proxy health recovery.

### Testing 🧪
- Added HTTP transport regression tests for direct, HTTP proxy, and SOCKS profiles with environment proxies set.
- Added crawler regression tests for payment-required and proxy authentication failures.
- Added crawler regression tests for critical CAPTCHA rotation and diagnostic candidate exhaustion.
- Added tests for stale-generation successes clearing cooldown while preserving provider cursor state.
- Expanded coverage for proxy lease selection, retry handling, and critical proxy diagnostics.

### Docs 📚
- Updated ARCHITECTURE.md with detailed proxy transport and retry logic explanations.
- Enhanced README.md with new features and proxy lease selector behavior.

## [v0.15.4] - 2026-05-19

### Features ✨
- Add `RetryDecision.ProxyFailureSeverity` to distinguish normal rotate-proxy retries from critical proxy failure cooldowns.
- Introduce `ReportProxyRetry` to rotate proxies without recording health failures.
- Release crawler proxy leases on neutral terminal response paths to avoid stale reservations.

### Improvements ⚙️
- Clarify retry proxy severity documentation in architecture, README, and issues docs.
- Rotate proxies immediately after normal retry decisions without marking them as failures.
- Reuse least-reserved healthy proxy lease under concurrency saturation.
- Reject trailing YAML documents in configfile loads instead of ignoring.

### Bug Fixes 🐛
- Fix rotate-proxy normal retries not respecting cooldown behavior correctly; only critical failures trigger cooldown now.
- Prevent stale proxy reservations by releasing leases on neutral responses like binary responses or no-title exhaustion.

### Testing 🧪
- Add coverage tests for rotate-proxy retry cooldown and normal retry reports.
- Add crawler regression tests for proxy lease release on neutral terminal responses.
- Add coverage for saturated proxy lease selection and multi-document YAML stream loads.

### Docs 📚
- Update architecture, changelog, README, and issues documentation with new proxy retry severity handling and lease selector behavior.
- Clarify platform hooks for retry severity and their proxy health impact.

## [v0.15.3] - 2026-05-12

### Features ✨
- _No changes._

### Improvements ⚙️
- Release neutral terminal crawler proxy leases properly to avoid stale reservations on paths like binary responses, page-not-found titles, no-title exhaustion, and incomplete-content exhaustion.
- Rotate proxy providers immediately after reported failures while releasing neutral terminal responses without affecting proxy health.
- Reuse least-reserved healthy proxy leases under concurrency saturation.

### Bug Fixes 🐛
- Fix proxy lease finalization to release leases on neutral terminal responses, preventing stale reservations and improving lease management.

### Testing 🧪
- Add crawler response regression tests for neutral terminal proxy lease release scenarios.
- Add regression coverage for saturated proxy lease selection through the public HTTP proxy selector path.
- Add configfile regression for validation of multi-document YAML streams.

### Docs 📚
- Update architecture documentation to clarify proxy lease release on neutral terminal crawler responses.
- Update README to reflect improvements in proxy lease selection and release behavior.

## [v0.15.2] - 2026-05-11

### Features ✨
- Add strict YAML configuration loading with scalar-only environment variable interpolation.
- Introduce explicit errors for missing environment variables and trailing YAML documents.
- Preserve and reuse proxy leases across redirects to improve proxy rotation.

### Improvements ⚙️
- Reuse the least-reserved healthy proxy lease when all leases are already reserved to handle concurrency saturation.
- Unify crawler proxy lease rotation and release neutral crawler proxy leases.
- Enhance proxy lease selection to keep leases sticky on success and rotate immediately on failure.

### Bug Fixes 🐛
- Fix proxy lease reuse logic to avoid treating all reserved leases as exhausted.
- Reject trailing YAML documents in config loads instead of silently ignoring them.

### Testing 🧪
- Add extensive regression tests for proxy lease selection under saturated conditions.
- Add tests covering multi-document YAML rejection and scalar interpolation in config files.

### Docs 📚
- Document new `configfile` package features including strict YAML loading and environment interpolation.
- Update README with new `configfile` usage and proxy lease selector behavior improvements.

## [v0.15.1] - 2026-05-01

### Features ✨
- Add `crawler.ProxyLeaseAttemptScope` for operation-scoped failed-lease tracking and exhausted-candidate detection.

### Improvements ⚙️
- Expose proxy candidate counts from `ProxyLeaseSelector` for callers that need bounded per-operation retry loops.

### Bug Fixes 🐛
- Prevent scrape/request batches from reacquiring leases that already failed inside the same operation when callers opt into the attempt scope.

### Testing 🧪
- Add crawler tests for failed-lease skipping, exhausted-candidate errors, nil scopes, nil selectors, and invalid leases.

### Docs 📚
- Document crawler proxy rotation and attempt-scope behavior in README and architecture notes.

## [v0.15.0] - 2026-04-30

### Features ✨
- Introduce Proxy Lease Selector for managing proxy rotations with provider/user awareness.
- Add API to acquire, release, and report success or failure of proxy leases.
- Attach proxy lease selection directly to HTTP requests for seamless proxy usage.

### Improvements ⚙️
- Enforce validation of provider and user names, and proxy URLs in proxy configurations.
- Support reservation counting to manage concurrent lease usage properly.
- Expand compatibility with string-only proxy failure/success reports.
- Return typed error when no proxy lease is available for required acquisition.

### Bug Fixes 🐛
- Reject empty or invalid proxy URLs during lease selector creation.
- Ignore stale reports after generation changes to maintain selector consistency.
- Prevent panics and handle nil selector cases gracefully during lease operations.

### Testing 🧪
- Add comprehensive unit tests covering lease acquisition, rotation, reuse, validation, and edge cases.
- Test fallback behaviors when selectors are nil or leased proxies invalid.
- Verify request-based lease acquisition attaches correct proxy selection context.

### Docs 📚
- Update issues documentation with relevant notes for proxy lease selector usage and issues.

## [v0.14.1] - 2026-04-28

### Features ✨
- _No changes._

### Improvements ⚙️
- Enhance proxy rotator to keep using the current healthy proxy until it fails before switching to the next proxy.

### Bug Fixes 🐛
- Fix proxy rotator logic to correctly evaluate and attach proxies, improving proxy rotation reliability.

### Testing 🧪
- Add tests to verify proxy rotator sticks to successful proxies until failure.
- Update existing proxy rotator tests to support scenarios without health tracker.

### Docs 📚
- _No changes._

## [v0.14.0] - 2026-04-28

### Features ✨
- Added crawler package with HTTP and browser transports.
- Introduced proxy rotation selector for provider/user-aware proxy management.
- Supported proxy rotation with sticky selection that advances on failure.

### Improvements ⚙️
- Provided browser transport wrappers exposing browser launch, page rendering, and Chrome version detection utilities.
- HTTP transport facade added to configure and create HTTP clients with proxy profiles.
- Enhanced proxy rotation selector with thread-safe provider and user rotation logic and context-attached proxy metadata.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Added comprehensive tests for browser transport facade covering profile inference, session management, and rendering.
- Tested HTTP transport facade for profile inference, normalization, client creation, and SOCKS proxy detection.
- Unit tested proxy rotation selector behavior including failure/success recording, selection stickiness, and provider-user rotations.

### Docs 📚
- Documented crawler package describing its crawling capabilities, proxy rotation support, and transport helpers for HTTP and browser clients.

## [v0.13.0] - 2026-04-27

### Features ✨
- Add reusable HTTP client transport with proxy support in a new `httptransport` package.
- Support SOCKS proxy and HTTP proxy seamlessly with automatic transport configuration.

### Improvements ⚙️
- Refactor HTTP client profile handling and normalization through the new `httptransport` package.
- Replace inline HTTP client implementation with calls to `httptransport` for cleaner code and easier maintenance.
- Centralize proxy URL parsing, validation, and transport ID inference in `httptransport`.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Add comprehensive tests for the new HTTP transport client covering direct, HTTP, and SOCKS proxy scenarios.
- Remove older HTTP client tests that injected failure branches now covered by `httptransport` tests.

### Docs 📚
- _No changes._

## [v0.12.1] - 2026-04-03

### Features ✨
- _No changes._

### Improvements ⚙️
- _No changes._

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Add executable billing examples that exercise `Service` and `WebhookHandler` flows as part of the test suite.

### Docs 📚
- Expand package-level `billing` documentation with integration guidance and a clearer package-layer overview.
- Add GoDoc comments for exported billing types, interfaces, constructors, and helpers.

## [v0.12.0] - 2026-04-03

### Features ✨
- Generalize the shared `billing` package to support data-driven plan and pack catalogs across products.
- Add `CustomerContext` and subject-aware billing metadata so checkout and webhook flows can carry both email and stable app subject IDs.
- Support pack-only billing products and configurable top-up eligibility policies in the shared billing service.
- Export shared billing compatibility types and metadata helpers for app-specific adapters.

### Improvements ⚙️
- Unify Paddle and Stripe checkout metadata generation, subscription inspection, and reconcile helpers around the shared billing core.
- Add subscription inspection helpers for canonical provider-state selection and active-subscription detection.
- Preserve compatibility with legacy billing metadata keys used by existing Poodle Scanner and LLM Crossword flows.
- Make Chrome version and default user-agent detection in `browsertransport` directly testable without changing runtime behavior.

### Bug Fixes 🐛
- Simplify Paddle timestamp parsing to rely on the Go RFC3339 parser’s native fractional-second support.
- Remove an unreachable shared billing HTTP error mapping branch tied to legacy compatibility aliases.
- Tighten Stripe checkout-sync ordering coverage around timestamp and event-id sorting paths.

### Testing 🧪
- Restore `billing` package coverage to 100% with focused tests for generalized catalog handling, inspection paths, shared service policies, and sync/reconcile helpers.
- Restore `browsertransport` package coverage to 100% with direct tests for Chrome-version detection and user-agent fallback behavior.
- Add direct Stripe checkout-session list coverage for pagination, request failures, and cursor validation.

### Docs 📚
- _No changes._

## [v0.10.0] - 2026-03-28

### Features ✨
- Introduce `browsertransport` package: reusable proxy-aware browser and HTTP transport runtime for scraping workloads with support for browser profiles, sessions, SOCKS forwarding, and one-shot rendering.
- Add `RenderPages` helper for concurrent multi-URL rendering.
- Add `NewHTTPClient` to build HTTP clients bound to transport profiles.

### Improvements ⚙️
- Extract shared browser transport runtime from prior evaluation code.
- Honor caller cancellation during tab initialization to improve responsiveness.

### Bug Fixes 🐛
- Fix IPv6 SOCKS dial target formatting issue.

### Testing 🧪
- Add extensive tests for `browsertransport` package including profiles, sessions, proxy handling, SOCKS forwarder, and render helpers.

### Docs 📚
- Document browser transport architecture and usage in `ARCHITECTURE.md` and `README.md`.
- Add explanation of browser rendering stack and transport profiles.

## [v0.9.1] - 2026-03-27

### Features ✨
- _No changes._

### Improvements ⚙️
- Attach HTTP proxy authentication to the dedicated render target context to fix proxy auth errors when using a dedicated render tab.
- Refactor `RenderPage` to properly sequence proxy auth setup on the render context.

### Bug Fixes 🐛
- Fix proxy authentication lost due to rendering on a derived context without auth handler.
- Add error handling for enabling fetch-based proxy auth.

### Testing 🧪
- Add regression tests to verify proxy auth setup uses the render target context.
- Test handling of fetch enable errors in HTTP proxy auth.

### Docs 📚
- Document issue UT-304 regarding jseval proxy auth and render target binding.

## [v0.9.0] - 2026-03-27

### Features ✨
- Introduced a new dual-provider billing package supporting Paddle and Stripe billing integrations.
- Added comprehensive webhook processors for subscription status, grant processing, and webhook chaining.
- Included full coverage tests for jseval proxy, SOCKS5 forwarder, and authentication modules.

### Improvements ⚙️
- Enhanced billing package for data race prevention, improved security, TOCTOU race fixes, and better error handling.
- Reformatted billing test files and improved static code checks, including typed nil context usage in tests.
- Optimized Paddle webhook grant resolver to skip invalid plans and packs with empty or zero credits.

### Bug Fixes 🐛
- Fixed chromedp panic in fetchEnable test by injecting chromedpRunner.
- Addressed several subscription lifecycle event handling edge cases, including stale events and metadata resolution errors.
- Corrected fallback logic for subscription price ID resolution from nested item structures.

### Testing 🧪
- Achieved 100% test coverage for proxy, SOCKS5 forwarder, and auth in jseval tests.
- Added extensive unit and integration tests for billing providers, webhook processors, and subscription state repository.
- Included tests covering error scenarios such as repository unavailability and customer email resolution failures.

### Docs 📚
- Added documentation stubs for the new billing package components and billing JSON handling.

## [v0.5.2] - 2026-03-23

### Features ✨
- _No changes._

### Improvements ⚙️
- Add extensive unit tests across crawler and utils packages for improved coverage and reliability.
- Introduced Makefile with commands for formatting, linting, testing, coverage, and CI integration.
- Refined crawler constants formatting for better code consistency.

### Bug Fixes 🐛
- Restore error handling logic that was previously removed during test coverage improvements.

### Testing 🧪
- Add a large suite of new tests covering crawler package internals, proxies, product creation, configuration validation, file persistence, and more.
- Include integration and unit tests with improved test coverage metrics.

### Docs 📚
- _No changes._

## [v0.4.0]

### Features ✨
- **Breaking**: Rewrite `crawler` package with production engine extracted from PoodleScanner.
- Add `ResponseHandler` interface for pluggable response processing.
- Add `DefaultResponseHandler` with generic parse-evaluate-emit flow.
- Add `PlatformHooks` interface for title normalisation and retry decisions.
- Add `Target` type with smart constructor and extensible `Metadata` map (replaces `Page`).
- Add proxy rotation (round-robin) with circuit-breaker health tracking.
- Add HTTP transport chain: idle-timeout, context-aware, panic-safe wrappers.
- Add file persistence with async background worker pool.
- Add request configurator with header/cookie injection.

### Improvements ⚙️
- Restructure `Config` into `ScraperConfig` and `PlatformConfig` sub-types.
- Export retry handler, proxy rotator, transport constructors, and response helpers for custom `ResponseHandler` implementations.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Add `TestNewTarget` smart constructor validation test.
- Add `TestConfigValidation` for config edge cases.
- Add `TestSanitizeProxyURL` for proxy URL credential stripping.

### Docs 📚
- Add package-level documentation in `doc.go`.

## [v0.3.0]

### Features ✨
- Add `crawler` package — a generic, reusable Colly-based web crawler with concurrent page fetching, retries with exponential backoff, rate limiting, and pluggable document evaluation via the `Evaluator` interface.

### Improvements ⚙️
- Upgrade CI workflow to `actions/checkout@v4` and `actions/setup-go@v5` with `go-version: 'stable'`.
- Fix GitHub Actions `allowed_actions` configuration that was blocking all CI runs.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Add crawler integration tests: basic crawl with CSS selector evaluation, and retry with exponential backoff on server errors.

### Docs 📚
- _No changes._

## [v0.2.1]

### Features ✨
- _No changes._

### Improvements ⚙️
- _No changes._

### Bug Fixes 🐛
- Prevent duplicate side effects under worker contention by adding an optional scheduler claim hook (`ClaimingRepository`) that skips dispatch when claim ownership is lost.

### Testing 🧪
- Add scheduler regression tests for lost-claim and claim-error scenarios to ensure dispatch is skipped deterministically.

### Docs 📚
- _No changes._

## [v0.2.0]

### Features ✨
- Add preflight reporting helpers for shared service tooling.
- Move validation logic to edge.

### Improvements ⚙️
- Introduce Viper adapter for YAML configuration loading and redaction.
- Add support for redacted configuration reporting with stable hash fingerprints.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Add tests for preflight report generation and dependency checks.
- Validate service info requirements and error scenarios.

### Docs 📚
- Add comprehensive preflight package documentation explaining report structure, redaction, and usage.

## [v0.1.3]

### Features ✨
- Add autonomous agentic flow with LLMs for advanced user scenarios

### Improvements ⚙️
- Enforce Go formatting, vetting, staticcheck, and ineffassign checks in CI pipeline
- Add detailed architecture documentation to clarify package design and boundaries
- Introduce comprehensive Git and Go agent guidelines for workflow and coding standards

### Bug Fixes 🐛
- Validate JSON schema response before sending in LLM client to prevent invalid requests
- Handle nil context gracefully in llm Factory.Chat method to avoid panics
- Properly close response body on HTTP Do errors to prevent resource leaks

### Testing 🧪
- Expanded LLM-related tests to cover additional edge cases and error handling

### Docs 📚
- Add ARCHITECTURE.md to explain repository design and package responsibilities
- Add AGENTS.md, AGENTS.GIT.md, and AGENTS.GO.md documents outlining workflows, git policies, and Go best practices

## [v0.1.1]

### Features ✨
- Added a generic retry worker for scheduling jobs with exponential backoff and persistent attempt tracking.
- Introduced ExpandEnvVar function to expand environment variables with trimming.
- Migrated repository module path to the `tyemirov` namespace.

### Improvements ⚙️
- Added GitHub Actions workflow to run Go tests on pull requests.
- Enhanced README with detailed package descriptions and usage examples for each utility.
- Added helpers for file operations, math calculations, text normalization, environment variable management, and pointer utilities.
- Improved logging and error handling in file operations.
- Upgraded Go version to 1.25.4.

### Bug Fixes 🐛
- _No changes._

### Testing 🧪
- Added tests covering the new scheduler worker's retry and dispatch logic.

### Docs 📚
- Expanded README with comprehensive package and function documentation.
- Documented all new utility functions and modules with usage examples.

## [v0.0.5] - 2025-01-25
### What's New 🎉~
- Feature 1: ExpandEnvVar function expands an environmental variable

## [v0.0.5] - 2025-01-25
### What's New 🎉~
- Feature 1: GetEnvOrFail function added to retrieve an environmental variable or fail

## [v0.0.3] - 2025-01-19
### What's New 🎉~
- Feature 1: SanitizeToCamelCase function added to be used for CSS ids etc
- _Some_ tests added

## [v0.0.2] - 2025-01-18
### What's New 🎉~
- Feature 1: File, Math and Text utilities moved to their own packages

## [v0.0.1] - 2025-01-18
### What's New 🎉~
- Feature 1: File, Math and Text utilities added
