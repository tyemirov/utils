# Architecture

`github.com/tyemirov/utils` contains reusable Go packages for application tasks.
The [README catalog](README.md#find-a-library) is the entry point for package selection.
Each package guide describes its public API, examples, and application responsibilities.

## Module Structure

The repository has one Go module and one release process.
Consumers import individual packages and select one version of the module.
A package guide gives a library its own documentation within this structure.

The module remains together while its packages can use the same ownership, dependency
updates, validation, and releases. Small helpers remain in this module.

## Package Relationships

The following relationships come from production imports within this repository:

| Package | Other packages from this module |
| --- | --- |
| `crawler` | `browsertransport`, `httptransport` |
| `browsertransport` | `httptransport` |
| `runtimeconfig` | `configfile` |
| `jseval` | `browsertransport` |
| `cmd/configenvcheck` | `configfile` |

The other library packages have no production imports from this module.
External dependencies are declared in [go.mod](go.mod).

## Application Responsibilities

| Library | Shared responsibilities | Consumer responsibilities |
| --- | --- | --- |
| [Billing](billing/README.md) | Provider operations, subscription state, webhook processing | Authentication, HTTP routes, checkout interface, credit ledger |
| [Crawler](crawler/README.md) | Requests, proxy selection, retries, result delivery | Product selection, content rules, result storage |
| [Browser transport](browsertransport/README.md) | Browser sessions, render tabs, proxy connections | Browser installation, page readiness, session lifetime |
| [HTTP transport](httptransport/README.md) | HTTP client construction and proxy connections | Request content, response processing, operation lifetime |
| [Runtime configuration](runtimeconfig/README.md) | File selection, strict decode, application validation call | Config type, validation rules, selected file path |
| [Config file](configfile/README.md) | YAML parsing and environment interpolation | Target type, environment contract |
| [GAuss](gauss/README.md) | Consent URL, token exchange, authenticated HTTP client | OAuth callback, state verification, token storage |
| [LLM client](llm/README.md) | Chat request transport and retries | Endpoint selection, model selection, prompts |
| [Scheduler](scheduler/README.md) | Due-job selection and retry timing | Persistent job repository, dispatch, ownership claims |
| [Preflight](preflight/README.md) | Report assembly | Config redaction and dependency checks |

## Criteria for Separate Modules

A separate module is useful when a library needs one or more of these changes:

- Separate ownership or a different group of consumers.
- An separate release schedule because unrelated changes prevent consumer upgrades.
- A separate dependency or Go toolchain requirement.

Before extraction, record the affected consumers and the required public API.
Define the new module path, ownership, dependency boundary, and release checks.
Include all consumer import updates in the migration scope.
Use the new canonical imports after migration.

Billing is the first candidate for an extraction review. It has a clear
application purpose and no production imports from other packages in this module.
The crawler and transport packages form another candidate group because they
use common code. These candidates remain in the current module until a separate
implementation decision defines their migration.

## Browser and Proxy Behavior

`browsertransport` controls browser sessions, render tabs, authenticated proxy
connections, and one-shot page rendering. `httptransport` controls HTTP client
construction. Direct HTTP profiles bypass `HTTP_PROXY` and `HTTPS_PROXY`.
Callers select an explicit HTTP or SOCKS profile when a proxy is required.

`crawler.ProxyLeaseSelector` keeps successful leases and advances providers
after failures. When all healthy leases are reserved, it uses the least-reserved
healthy lease again. Neutral terminal responses release reservations without changes
to proxy health. Success from an older generation can clear proxy health without
reversal of the current provider cursor.

`ProxyLeaseAttemptScope` records failed leases for one operation. After every
candidate fails, acquisition returns `ErrProxyLeaseCandidatesExhausted`.
A rotation-only decision changes the lease without a proxy health penalty.
Platform hooks can set a critical severity when the proxy itself fails.
Provider credential failures quarantine the affected lease and select alternative candidates.
The [crawler guide](crawler/README.md#proxy-selection) links the public contracts and tests.

## Configuration and Reports

`runtimeconfig` uses `configfile` for strict YAML parsing and environment interpolation.
Applications supply typed contracts and validation rules. Consumers use the
resulting config and effective values after this boundary.
`preflight` assembles reports from application-supplied config reporters and dependency checkers.

## Validation

The [Makefile](Makefile) controls local checks. Package tests and the `test` package
exercise public behavior. Local protocol tests verify application contracts.
Live provider access and consumer deployment need separate qualification.
