# Config File: Strict YAML and Environment References

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/configfile`

Use `configfile` to decode YAML into a supplied Go type. It expands environment
references inside scalar values and rejects unknown fields, missing required
references, and trailing YAML documents.
For application file selection and validation, use [runtimeconfig](../runtimeconfig/README.md).

## Start

Save this example as `main.go` in your application module. Run `go run main.go`.
It uses an explicit environment lookup and prints `localhost:8080`.

```go
package main

import (
	"fmt"
	"log"

	"github.com/tyemirov/utils/configfile"
)

func main() {
	var config struct {
		Address string `yaml:"address"`
	}
	environment := map[string]string{"APP_ADDRESS": "localhost:8080"}
	options := configfile.EnvironmentOptions{
		Lookup: func(name string) (string, bool) {
			value, found := environment[name]
			return value, found
		},
	}
	err := configfile.LoadYAMLBytesWithOptions([]byte("address: ${APP_ADDRESS}\n"), &config, options)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(config.Address)
}
```

## Select an API

| Task | Entry point |
| --- | --- |
| Decode a file | `LoadYAML` |
| Decode bytes | `LoadYAMLBytes` |
| Supply an environment registry or lookup | `LoadYAMLWithOptions`, `LoadYAMLBytesWithOptions` |
| Expand references before application-specific decode | `InterpolateYAMLWithOptions` |
| Declare required and optional variables | `EnvContract`, `EnvRegistry` |
| Select a value schema | `EnvValueSchemaForKind` |

The application controls its config type, environment contract, and domain validation.
Use `$NAME` or `${NAME}` for environment references.
The [environment tests](configfile_test.go) show required variables, optional
variables, value schemas, and invalid input behavior.

## Configuration CLI

Install the command from your application environment:

```sh
go install github.com/tyemirov/utils/cmd/configenvcheck@latest
configenvcheck --help
```

Create `config.yml` with this content:

```yaml
address: ${APP_ADDRESS}
```

Supply the process value explicitly:

```sh
APP_ADDRESS=localhost:8080 configenvcheck --config config.yml --inherit-shell --schema APP_ADDRESS=hostport --show-registry
```

The command prints the required environment registry after successful validation.
Use `--env-file` for dotenv input, `--optional-env` for an optional variable,
and `--required-env` for an additional required variable.
The [command source](../cmd/configenvcheck/main.go) defines the accepted flags.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES='./configfile ./cmd/configenvcheck'
```

The [runtime configuration package](../runtimeconfig/README.md) and
`cmd/configenvcheck` import this package.
