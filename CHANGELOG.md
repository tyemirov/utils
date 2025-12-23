# Changelog

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
