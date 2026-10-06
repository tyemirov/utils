# MCP Authorization

`github.com/tyemirov/utils/mcpauth` connects an application token validator to the official MCP Go SDK.
It supplies typed principal access, explicit session binding, independent scope slices, and safe public errors.

The package uses the official SDK v1.8.0.
The utils module retains Go 1.26.0. The SDK requires Go 1.25.0 or later.

## Package Boundary

The application callback validates the token signature, issuer, audience, tenant, and account policy.
It supplies a principal, expiration, scopes, and session binding through `NewVerifiedToken`.
The package converts that result into SDK `auth.TokenInfo`.

The SDK owns bearer middleware, expiration, endpoint scopes, metadata, and protocol transport.
The application owns tool scopes, resource authorization, origins, request limits, and operation lifetime.
The package does not import TAuth or implement JWT validation.

## Construct A Verifier

Supply both callbacks through `Config`.
Use `Validate` for token validation and `ReportFailure` for application diagnostics.
The following function connects those callbacks to SDK middleware:

```go
type Account struct {
    Subject string
}

func protect(
    handler http.Handler,
    validate mcpauth.ValidateFunc[Account],
    reportFailure func(context.Context, error),
) (http.Handler, error) {
    verifier, err := mcpauth.NewVerifier(mcpauth.Config[Account]{
        Validate:      validate,
        ReportFailure: reportFailure,
    })
    if err != nil {
        return nil, err
    }
    return auth.RequireBearerToken(verifier.Verify, &auth.RequireBearerTokenOptions{
        ResourceMetadataURL: "https://example.com/.well-known/oauth-protected-resource/mcp",
        Scopes:              []string{"example:read"},
    })(handler), nil
}
```

This fragment uses `context`, `net/http`, `utils/mcpauth`, and SDK `auth` imports.
[The executable example](examples_test.go) exercises the public API without an external provider.
Applications must supply a real validator for production requests.

## Supply Verified Token Data

Call `NewVerifiedToken` only after application validation succeeds.
Supply the exact selected session binding without normalization.
An empty binding or zero expiration produces a constructor error.
Expired tokens remain subject to SDK expiration enforcement.
The adapter adds no expiration check.

The package copies scopes at construction and during each SDK conversion.
A callback can reuse one constructed result without shared SDK scope slices.
The application must treat principal data as immutable during each request.
The package does not copy arbitrary principal data.

Use the session binding to distinguish authorization relations.
An account subject alone can be insufficient for stateful sessions.
For example, an application can bind issuer, tenant, subject, client, grant, and provider identity.
Keep that binding stable across token refresh within the same relation.
Do not use the raw access token or its hash as the binding.

## Obtain The Principal

For an HTTP handler, obtain `auth.TokenInfoFromContext(request.Context())`.
For an MCP tool, obtain `request.Extra.TokenInfo`.
Pass that value to `mcpauth.Principal[Account]`.
The accessor returns an error for absent principal data or an incorrect principal type.

```go
principal, err := mcpauth.Principal[Account](request.Extra.TokenInfo)
if err != nil {
    return nil, err
}
```

The package keeps its SDK context key private.
Do not construct or modify that entry directly.
Apply application resource authorization after principal access.

## Failure Contract

| Condition | HTTP result through SDK middleware | Diagnostic behavior |
| --- | --- | --- |
| Missing or malformed bearer credential | `401` | SDK behavior |
| `ErrUnauthenticated`, including wrapped values | `401` | No operational failure report |
| Expired token with required scopes | `401` | SDK behavior |
| Missing endpoint scope | `403` | SDK behavior |
| Unexpected validator error | `500` with a fixed public error | Report the original error once |
| Successful callback without a constructed token | `500` with a fixed public error | Report the contract error once |

Return `ErrUnauthenticated` for a known credential failure.
Do not return provider error text as a public rejection message.
Unexpected errors retain their original cause in the failure reporter.
The application must remove credentials before it records diagnostics.
The package stores no raw bearer credential by default.
SDK v1.8.0 evaluates endpoint scopes before expiration.
A token that lacks scopes and is expired returns `403` through that middleware.
The adapter retains SDK precedence without another expiration check.

Construct a verifier before use.
A nil or zero verifier returns a fixed operational error without a panic.
That verifier has no configured reporter and cannot report the failure.

The callback receives the original request and context.
It must obey cancellation and support concurrent requests.
The adapter does not retry validation or cache authorization decisions.

## Connect TAuth In An Application

Construct `tauth/pkg/oauthvalidator` in the application startup path.
Use the configured issuer, audience, and JWKS URL.
Keep its `RequiredScopes` empty for this adapter path.
Use SDK middleware for endpoint scopes and application handlers for operation scopes.

The callback can convert the TAuth result as follows:

```go
claims, err := validator.ValidateToken(ctx, credential)
if errors.Is(err, oauthvalidator.ErrInvalidToken) {
    return nil, mcpauth.ErrUnauthenticated
}
if err != nil {
    return nil, err
}
if claims.TenantID != expectedTenant {
    return nil, mcpauth.ErrUnauthenticated
}
principal, err := resolveAccount(ctx, claims)
if err != nil {
    return nil, err
}
return mcpauth.NewVerifiedToken(
    sessionBinding(claims),
    claims.ExpiresAt.Time,
    strings.Fields(claims.Scope),
    principal,
)
```

This callback fragment uses application-owned `resolveAccount` and `sessionBinding` functions.
Those functions select the account policy and session relation.
It does not add a TAuth dependency to utils.

TAuth currently classifies JWT parse and key retrieval failures as `ErrInvalidToken`.
The adapter cannot recover operational causes absent from that error.
TAuth OAuth claims do not supply email.
Applications with email account keys need a trusted subject-to-account association.

SDK bearer middleware does not enforce every application HTTP policy.
Retain application rejection of duplicate `Authorization` headers when that policy applies.
Retain application challenge parameters such as `error="insufficient_scope"` when required by the public contract.

## Validation

Run the focused public HTTP tests, race detector, and coverage report:

```sh
make test-mcpauth
```

The suite uses local HTTP servers, real SDK middleware, and the official SDK client.
Injected validator results exercise rejection, operational failures, principal access, scopes, and session isolation.
They prove the adapter contract rather than provider connectivity.
Applications must verify real token validation and domain authorization during consumer adoption.

Run the complete module checks and 100 percent package coverage gate:

```sh
make ci
```

See [the implementation design](../.mprlab/MCP-INTEGRATION.md) for consumer responsibilities and future adoption stages.
