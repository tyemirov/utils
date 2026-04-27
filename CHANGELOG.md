# Changelog

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
