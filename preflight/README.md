# Preflight: Configuration and Dependency Reports

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/preflight`

Use `preflight` to assemble a versioned JSON report before service launch.
Applications supply effective configuration, service metadata, and dependency checks.

## Start

Save this example as `main.go` in your application module. Run `go run main.go`.
The output is a JSON report with an empty dependency list.

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/tyemirov/utils/preflight"
)

type exampleReporter struct{}

func (exampleReporter) Build(mode preflight.RedactionMode) (json.RawMessage, error) {
	return json.RawMessage(`{"address":"localhost:8080"}`), nil
}

func main() {
	service, err := preflight.NewServiceInfo("example", "dev", "local", "", "1", "1")
	if err != nil {
		log.Fatal(err)
	}
	report, err := preflight.BuildReport(
		context.Background(), "1", service, exampleReporter{}, nil,
		preflight.RedactionModeRedacted,
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(report))
}
```

The example config contains no secrets. An application reporter must implement
redaction for its sensitive values.

## Report Contract

| Field | Source |
| --- | --- |
| `schema_version` | Version supplied to `BuildReport` |
| `service` | Metadata from `NewServiceInfo` |
| `effective_config` | JSON from `ConfigReporter.Build` |
| `dependencies` | Results from each `DependencyChecker.Check` |

`RedactionModeRedacted` requests redaction from the reporter.
`BuildReport` passes the mode to the reporter and includes the returned JSON.
`HashSHA256Hex` supplies fingerprints when a report needs value comparison.

The application controls config redaction, dependency checks, and readiness decisions.
Use [runtimeconfig](../runtimeconfig/README.md) to load config before report assembly.
A successful report build establishes report validity. Dependency readiness
comes from the `Ready` fields supplied by the checkers.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./preflight
```

See [report contracts](preflight.go) and [report tests](preflight_test.go).
