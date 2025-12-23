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

## Viper adapter

The `preflight/viperconfig` package loads YAML configuration with Viper,
applies configured environment bindings, and invokes a `Redactor` to build the
effective config payload.
