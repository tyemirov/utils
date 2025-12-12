# Changelog

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
