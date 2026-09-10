# Runtime Configuration: One Typed Application Config

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/runtimeconfig`

Use `runtimeconfig` to select a YAML file, decode a typed config, and apply
application validation. It uses [configfile](../configfile/README.md) for strict
parsing and environment interpolation.

## Start

Create `config.yml` in your application module:

```yaml
address: localhost:8080
```

Save this example as `main.go` in the same directory. Run `go run main.go`.
The output is `localhost:8080`.

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/tyemirov/utils/runtimeconfig"
)

type applicationConfig struct {
	Address string `yaml:"address"`
}

func main() {
	loader, err := runtimeconfig.NewLoader(runtimeconfig.Contract[applicationConfig]{
		Validate: func(config applicationConfig) error {
			if config.Address == "" {
				return errors.New("address is required")
			}
			return nil
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	loaded, err := loader.Load("config.yml")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(loaded.Config.Address)
}
```

## Select an API

| Task | Entry point |
| --- | --- |
| Define the config type and validation | `Contract[T]`, `NewLoader[T]` |
| Read a complete config | `Loader.Load` |
| Read one required section | `Loader.LoadSection` |
| Select the explicit or default path | `Loader.ResolvePath` |
| Map scalar paths to named values | `ValueMapping`, `Loaded.Values` |
| Inspect the effective config | `Loaded.Config`, `Loaded.Settings`, `Loaded.EffectiveYAML` |

The application controls command flags, config types, validation rules, and the
selected path. Environment values enter through YAML interpolation.
The default file name is `config.yml` when the caller supplies an empty path.
Effective YAML and settings can contain secrets. Apply application redaction
before their inclusion in [preflight reports](../preflight/README.md).

## Consumer Source Examples

MediaOps imports this package in `internal/runtimeconfig/config.go` and
`internal/runtimeconfig/values.go`. LoopAware imports it in
`internal/serverconfig/config.go`. These source examples were checked on
2026-09-10. They show source usage, with deployment verified separately.

## Validation

Run tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./runtimeconfig
```

The [package tests](runtimeconfig_test.go) cover typed config, value maps,
section loading, environment references, and validation failures.
