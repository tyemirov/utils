# Shared MCP Authorization

Status: P001 defined this design. F104 completed the shared package without consumer adoption.

The shared package lives at `github.com/tyemirov/utils/mcpauth`.
It connects an application token validator to the official MCP Go SDK.
It remains in the existing Go module and release process.
The package uses SDK v1.8.0 with the existing Go 1.26.0 module toolchain.

## Requirements And Evidence

The user selected `tyemirov/utils` as the implementation repository.
This design covers the shared package and sequential consumer adoption.
F104 implements only the shared package and its tests.
Consumer adoption, publication, and deployment remain separate work.

| Source | Confirmed behavior | Design consequence |
| --- | --- | --- |
| [utils architecture](../ARCHITECTURE.md) | Packages share one module and release process. | Add a root package without a nested module. |
| [TAuth dependencies](../../TAuth/go.mod) | TAuth already requires utils. | Keep TAuth imports outside utils. |
| [TAuth validator](../../TAuth/pkg/oauthvalidator/validator.go) | A public validator checks signed access tokens. | Supply this validator through an application callback. |
| [ISSUES.md adapter](../../ISSUES.md/internal/mcpaccess/access.go) | The adapter combines SDK authorization with GitHub policy and stateful sessions. | Share identity conversion and preserve application policy. |
| [LLM Proxy adapter](../../llm-proxy/internal/proxy/mcp.go) | The adapter combines SDK transport with tenant policy and generation services. | Share authorization conversion without moving execution services. |
| [LoopAware user model](../../loopaware/internal/model/models.go) | The user key is an email address. | Define a trusted OAuth account association before tool access. |

The three applications can use TAuth's released `pkg/oauthvalidator`.
LoopAware already requires TAuth v1.2.7, which contains that package.
ISSUES.md and LLM Proxy already use the official MCP Go SDK v1.7.0.
These versions describe the inspected source. They are not new dependency requirements.

TAuth currently imports `utils/preflight`.
A reverse module dependency would couple dependency updates and releases.
The inspected imports do not establish a Go package import cycle.
The proposed callback avoids this reverse dependency.

## Ownership

| Owner | Responsibilities |
| --- | --- |
| Official MCP Go SDK | Protocol transport, bearer middleware, endpoint scope checks, metadata responses, and tool registration |
| `utils/mcpauth` | Constructed token data, SDK identity conversion, typed principal access, and error translation |
| TAuth | Login, consent, token issuance, token validation, public keys, refresh, and revocation |
| Application validator callback | Trusted issuer, audience, tenant, token policy, principal construction, and session binding |
| Application handlers | Account association, resource ownership, operation scopes, tool schemas, audit data, and execution lifetime |

Use the SDK metadata handler and Streamable HTTP handler directly.
Keep HTTP routes, origins, request limits, protocol selection, and cancellation policy in each application.
Keep per-tool scope checks and audit hooks outside the first package.
Do not create a second JWT validator, JWKS client, tool registry, or protocol implementation.

## Public API

The following declarations describe the implemented API.

```go
type ValidateFunc[P any] func(
    context.Context, string, *http.Request,
) (*VerifiedToken[P], error)

type Config[P any] struct {
    Validate      ValidateFunc[P]
    ReportFailure func(context.Context, error)
}

func NewVerifier[P any](Config[P]) (*Verifier[P], error)

func NewVerifiedToken[P any](
    bindingID string,
    expiresAt time.Time,
    scopes []string,
    principal P,
) (*VerifiedToken[P], error)

func (*Verifier[P]) Verify(
    context.Context, string, *http.Request,
) (*auth.TokenInfo, error)

func Principal[P any](*auth.TokenInfo) (P, error)

var ErrUnauthenticated error
```

`auth` denotes `github.com/modelcontextprotocol/go-sdk/auth`.
`VerifiedToken` and `Verifier` keep their fields private.
`Verify` must be assignable to `auth.TokenVerifier`.

- Require both callbacks when constructing a verifier.
- Reject an empty session binding and a zero expiration value at construction.
- Copy scopes into each constructed token and each returned SDK `TokenInfo`.
- Reject a successful callback result without a constructed token.
- Store the supplied binding identifier as SDK `TokenInfo.UserID`.
- Store the principal through one private SDK context key.
- Return an error for absent principal data or an incorrect principal type.
- Keep the principal type under application ownership.
- Do not modify the supplied principal.

The application must treat a principal as immutable during its request.
The package does not create a deep copy of arbitrary principal data.
HTTP middleware can obtain `TokenInfo` through the SDK context accessor.
Tool handlers can obtain it through `request.Extra.TokenInfo`.

### Error Contract

| Condition | Shared behavior | HTTP result through SDK middleware |
| --- | --- | --- |
| Missing or malformed bearer credential | Let the SDK reject the request. | `401` |
| Callback returns `ErrUnauthenticated` | Return SDK `auth.ErrInvalidToken`. | `401` |
| The token is expired and has required scopes | Let the SDK enforce expiration. | `401` |
| Valid token lacks an endpoint scope | Let the SDK enforce endpoint scopes. | `403` |
| Callback returns an unexpected error | Report the failure and return a fixed public error. | `500` |
| Callback returns invalid token data without an error | Report the contract failure and return a fixed public error. | `500` |

Recognize wrapped `ErrUnauthenticated` values with `errors.Is`.
Never return the original operational error to SDK middleware.
The inspected SDK writes verifier error text to the HTTP response.
The failure reporter receives the original error for application diagnostics.
The application must remove credentials from those diagnostics.
Do not store the raw bearer credential in shared token data or shared logs.
An application principal can contain a credential only when its existing operation requires that credential.

### Validator And Session Contract

Construct the real TAuth validator during application startup.
Set its issuer, audience, and JWKS URL from the application configuration.
Use no TAuth `RequiredScopes` for this adapter path.
Use SDK middleware for endpoint scopes and application handlers for operation scopes.
Reject a wrong tenant in the application callback.
Translate known credential failures into `ErrUnauthenticated`.
Preserve operational failures as errors until the shared adapter removes public details.

The callback must receive the request context and obey cancellation.
The shared adapter must not retry token validation or cache authorization decisions.
TAuth retains its existing JWKS cache behavior.
TAuth currently converts JWT parse and key retrieval failures into `ErrInvalidToken`.
The adapter cannot recover operational causes that TAuth does not return.
The generic `500` contract applies only when the callback returns a distinct operational error.
TAuth validates expiration before the SDK validates expiration again.
The shared adapter must not add another expiration check.
SDK v1.8.0 evaluates endpoint scopes before expiration.
If both conditions fail, its middleware returns `403`.

A session binding is different from an account identifier.
ISSUES.md binds sessions to issuer, tenant, subject, client, grant, and provider identity.
Preserve that exact relation when converting its current adapter.
Do not substitute the subject alone or a hash of the raw access token.
Token refresh within the same authorized relation must preserve the selected binding.
Changes to that relation must not authorize an existing session.

## Implementation Sequence

Complete each stage before the next consumer migration.
Create separate implementation issues when implementation is authorized.
Keep feature work in Features and consumer refactors in Improvements.

### 1. Implement The Shared Package In utils

F104 completed this stage. The package guide defines its current public contract.
The package tests passed with 100 percent statement coverage and the race detector.
Final `make ci` passed with 100 percent statement coverage in every Go package.
Independent architecture review found no remaining blockers.

1. Run the applicable initial validation required by `.mprlab/POLICY.md`.
2. Add `make test-mcpauth` for the package's public HTTP tests.
3. Add a failing integration test before production code.
4. Add the official MCP Go SDK dependency through the Go package manager.
5. Record its resolved version and toolchain requirement.
6. Implement the constructors, verifier, principal accessor, and error translation.
7. Add package documentation and an application callback example.
8. Update the README catalog and architecture ownership table.
9. Run the focused target after each relevant change.
10. Run `make ci` after the last source change.

Expected files:

```text
mcpauth/doc.go
mcpauth/token.go
mcpauth/verifier.go
mcpauth/principal.go
mcpauth/verifier_integration_test.go
mcpauth/token_test.go
mcpauth/examples_test.go
mcpauth/README.md
go.mod
go.sum
Makefile
README.md
ARCHITECTURE.md
.mprlab/ISSUES.md
```

Keep the existing module toolchain unless the selected SDK requires a change.
Do not add TAuth to the utils dependency graph, including test dependencies.
Use an injected validator with controlled results for shared package tests.
Use real TAuth validation in consumer tests.
Document the TAuth callback without copying its validator implementation.

### 2. Adopt The Package In LLM Proxy

1. Record current HTTP authorization behavior through characterization tests.
2. Supply the existing validator and account principal through `mcpauth`.
3. Preserve tenant checks, ownership checks, origins, request limits, and cache headers.
4. Retain application middleware that rejects duplicate `Authorization` headers.
5. Retain `error="insufficient_scope"` in the application's `403` challenge.
6. Add characterization tests for both HTTP behaviors before the refactor.
7. Preserve generation budgets, cancellation, usage records, and durable operations.
8. Replace the old authorization conversion with the shared verifier.
9. Run `make test-mcp`, `make test-mcp-versions`, and `make test-mcp-oauth`.
10. Run the repository's final `make ci`.

Do not change protocol support as part of this authorization refactor.
The application retains its current explicit protocol contract.
SDK bearer middleware alone does not preserve the two specified header behaviors.

### 3. Adopt The Package In ISSUES.md

1. Record current scope, principal, and session behavior through characterization tests.
2. Keep the private GitHub principal and application token policy.
3. Supply the existing session binding through `mcpauth`.
4. Preserve per-tool scopes, schema validation, audit records, and product resources.
5. Remove the replaced verifier conversion from `internal/mcpaccess`.
6. Run `make test-mcp-protocol`, `make test-mcp-audit`, and `make test-mcp-oauth`.
7. Run the repository's final `make ci`.

The protected endpoint currently selects stateful MCP `2025-11-25`.
The SDK supports that contract. This proposal does not classify it as an SDK defect.

### 4. Add MCP Access To LoopAware

1. Define a trusted association between OAuth issuer, tenant, subject, and the existing account.
2. Create that association only through a verified account flow.
3. Reject an unresolved identity before access to application data.
4. Move selected operations and authorization into services shared by REST and MCP.
5. Add a stateless Streamable HTTP endpoint with the selected current protocol.
6. Use TAuth, SDK middleware, and `mcpauth` for authorization.
7. Add initial tools for site lists, feedback, traffic summaries, health, and error reports.
8. Preserve administrator, owner, creator, and team access rules.
9. Preserve aggregate analytics restrictions in every tool result.
10. Verify the OAuth flow and tools through local HTTP and browser tests.
11. Run LoopAware's final `make ci`.

TAuth OAuth claims do not contain email.
Do not infer email from the subject or accept an account association supplied by a tool caller.
LoopAware's identity flow and tool contract require a separate application design before this stage.

## Acceptance Criteria

Use a real SDK server and HTTP middleware in shared package tests.
Use the official SDK client for successful protocol requests.
Use direct HTTP requests for invalid credentials and response details.

| Area | Required evidence |
| --- | --- |
| Construction | Missing callbacks, empty binding, zero expiration, and invalid successful callback results cannot authorize requests. |
| Token conversion | The SDK receives the supplied expiration, copied scopes, binding, and typed principal. |
| Authentication | Missing credentials and known credential failures produce `401` with the configured metadata challenge. |
| Expiration | An expired token with required scopes produces `401` through SDK middleware. |
| Scope | A valid token without a required endpoint scope produces `403`. |
| Operational error | Unexpected failures produce `500` without the original error or credential in the response. |
| Diagnostics | The application reporter receives one operational failure per failed validation call. |
| Principal access | HTTP handlers and MCP tools obtain the same principal type and value. |
| Isolation | A different session binding cannot use an existing protected session. |
| Refresh | A refreshed token with the same authorized relation retains the selected session binding. |
| Concurrency | Each SDK result has an independent scope slice. Application principals remain immutable. |
| Cancellation | The supplied context cancellation reaches the validator callback. |
| Dependency | Package and module dependency inspection finds no TAuth dependency from utils. |

Run the existing 100 percent package coverage gate through `make ci`.
Add a focused race test to `make test-mcpauth` for concurrent identity conversion.
Use token expiration values well before or after the current time in shared HTTP tests.
The inspected SDK uses `time.Now()` without an injected clock.
Do not use short expiration intervals or require a replacement SDK clock.
Keep protocol dependencies local and deterministic.

Consumer tests must use the real TAuth validator with locally signed tokens and a local JWKS endpoint.
Verify wrong signatures, issuer, audience, tenant, expiration, and insufficient scopes through HTTP.
Verify application ownership separately from token validity.
For LoopAware, verify unresolved accounts and access attempts against another site's data.
For ISSUES.md, verify access attempts from another client or grant against an existing session.
Use local browser tests for the TAuth authorization flow.

Package source acceptance does not depend on publication or deployment.
Consumer upgrades require the shared package to be available through the selected dependency workflow.
Resolve changed MPR Lab dependencies through `@latest` during authorized consumer integration.
Do not commit local module replacements or copied shared code.
Keep publication and deployment as separately authorized operations.

## Open Decisions And Limits

The package location, dependency direction, API responsibilities, and first consumer order are selected in this proposal.
The following decisions belong to the later LoopAware implementation issue:

- Define the verified account association flow and any bounded data migration.
- Define exact tool names, schemas, scopes, and pagination.
- Define the public resource URL and TAuth client policy.

The initial LoopAware protocol proposal is stateless MCP `2026-07-28`.
Verify SDK and client support when that stage starts.
Use one current protocol contract without compatibility code.

The F104 package tests verify the adapter contract through local HTTP servers and the official SDK client.
Consumer tests must verify real token validation and application authorization during later adoption.
F104 does not verify consumer runtimes or production endpoints.
Production availability remains a separate operational result.

## References

- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [SDK transport and authorization](https://go.sdk.modelcontextprotocol.io/protocol/)
- [utils planning rules](PLANNING.md)
- [utils validation policy](POLICY.md)
