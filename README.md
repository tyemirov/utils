# Utils: Go Libraries for Application Tasks

`github.com/tyemirov/utils` contains Go libraries for payments, web crawling,
configuration, Google authorization, chat requests, and scheduled jobs.
Each package has its own import path. All packages use one module version
and one release process.

## Find a Library

| Your task | Package guide | Import path |
| --- | --- | --- |
| Accept payments through Stripe or Paddle | [Billing](billing/README.md) | `github.com/tyemirov/utils/billing` |
| Crawl pages, evaluate content, and rotate proxies | [Crawler](crawler/README.md) | `github.com/tyemirov/utils/crawler` |
| Render JavaScript pages through a browser | [Browser transport](browsertransport/README.md) | `github.com/tyemirov/utils/browsertransport` |
| Send HTTP requests through an explicit proxy | [HTTP transport](httptransport/README.md) | `github.com/tyemirov/utils/httptransport` |
| Load and validate application configuration | [Runtime configuration](runtimeconfig/README.md) | `github.com/tyemirov/utils/runtimeconfig` |
| Decode strict YAML and expand environment references | [Config file](configfile/README.md) | `github.com/tyemirov/utils/configfile` |
| Authorize Google access | [GAuss](gauss/README.md) | `github.com/tyemirov/utils/gauss` |
| Send chat-completion requests | [LLM client](llm/README.md) | `github.com/tyemirov/utils/llm` |
| Run scheduled jobs with retries | [Scheduler](scheduler/README.md) | `github.com/tyemirov/utils/scheduler` |
| Report configuration and dependency readiness | [Preflight](preflight/README.md) | `github.com/tyemirov/utils/preflight` |

### Small Helpers

| Your task | Package source | Import path |
| --- | --- | --- |
| Read lines, read files, or save HTML files | [File](file/file.go) | `github.com/tyemirov/utils/file` |
| Format numbers or calculate a probability result | [Math](math/math.go) | `github.com/tyemirov/utils/math` |
| Create a pointer to a floating-point value | [Pointers](pointers/pointers.go) | `github.com/tyemirov/utils/pointers` |
| Read required environment values or expand references | [System](system/env.go) | `github.com/tyemirov/utils/system` |
| Normalize text or create camelCase identifiers | [Text](text/text.go) | `github.com/tyemirov/utils/text` |
| Find the existing one-shot page API | [JSEval](jseval/jseval.go) | `github.com/tyemirov/utils/jseval` |

For new browser integrations, start with [browsertransport](browsertransport/README.md).
For typed application configuration, start with [runtimeconfig](runtimeconfig/README.md).

## Start

Use the Go version declared in [go.mod](go.mod).
From your application module, add the package that your task needs:

```sh
go get github.com/tyemirov/utils/configfile
```

This example decodes YAML into a typed config:

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
	if err := configfile.LoadYAMLBytes([]byte("address: localhost:8080\n"), &config); err != nil {
		log.Fatal(err)
	}
	fmt.Println(config.Address)
}
```

Save the example as `main.go` in your application module. Run `go run main.go`.
The output is `localhost:8080`.
The package guides contain further examples, prerequisites, and application responsibilities.

## Configuration CLI

[`cmd/configenvcheck`](cmd/configenvcheck/main.go) checks YAML environment references,
dotenv inputs, required variables, and value schemas.
See the [config file guide](configfile/README.md#configuration-cli) for its command syntax.

## Package Boundaries

The [architecture document](ARCHITECTURE.md) defines package relationships and
criteria for separate modules. The current structure keeps one repository
with task-specific package guides.

## Development

Run these targets from the repository root:

```sh
make test
make lint
make ci
```

The [Makefile](Makefile) defines format, lint, build, test, and coverage checks.
`make ci` runs all these checks. `make lint` needs `staticcheck` and `ineffassign`.
Package guides show focused test commands through the same Makefile.

## Release Lifecycle

- `make release` runs local CI and prepares a module archive and descriptor under `.git/mprlab-release`.
  It creates the local changelog commit and annotated SemVer tag.
- `make publish` verifies and publishes the prepared commit, tag, manifest, and module assets to GitHub.
- `make deploy` requests the published version from the Go module proxy.
  It verifies the origin commit and `go.mod` hash.

Each consumer controls its dependency upgrades.
Set `GO_MODULE_VERSION=vX.Y.Z` to select a published version that differs from the current `HEAD` tag.
Set `GO_MODULE_PROXY` to select a different proxy.
Use `DEPLOY_ARGS=--dry-run` to verify publication without activation of the proxy cache.

## License

See [LICENSE](LICENSE), [COMMERCIAL_LICENSE.md](COMMERCIAL_LICENSE.md), and
[CONTRIBUTOR_LICENSE.md](CONTRIBUTOR_LICENSE.md) for the applicable terms.
