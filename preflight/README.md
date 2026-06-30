# Preflight

The `preflight` package emits a versioned JSON report that captures a service's
effective configuration and dependency readiness before launch. It is designed
for external validators that need to compare configuration expectations without
running the service.

## Report shape

The report includes:
- `schema_version` plus `service` metadata (name, build, config schema version, endpoint contract)
- `effective_config` payload provided by a service-specific reporter
- `dependencies` list with readiness status and optional details

## Redaction

Use `RedactionModeRedacted` to strip sensitive fields while still reporting
hashes for comparison. The `HashSHA256Hex` helper is provided for stable
fingerprints (for example, signing keys or hostnames).

## Runtime config reports

Use `runtimeconfig` to load and validate the service config before launch, then
adapt the resulting effective settings or redacted payload behind the
`ConfigReporter` interface. The preflight package does not own config source
resolution.
