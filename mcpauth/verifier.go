package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// ErrUnauthenticated indicates a known credential failure. Validators can wrap it.
var ErrUnauthenticated = errors.New("unauthenticated")

var errVerificationFailed = errors.New("token verification failed")

// ValidateFunc validates a credential under application policy. It must obey
// context cancellation and return a token constructed by NewVerifiedToken.
type ValidateFunc[P any] func(context.Context, string, *http.Request) (*VerifiedToken[P], error)

// Config supplies application validation and operational failure reporting.
// Both callbacks are required. ReportFailure must remove credentials from logs.
type Config[P any] struct {
	Validate      ValidateFunc[P]
	ReportFailure func(context.Context, error)
}

// Verifier adapts application validation to SDK authorization. Its zero value
// cannot authorize a request. Construct it with NewVerifier.
type Verifier[P any] struct {
	validate      ValidateFunc[P]
	reportFailure func(context.Context, error)
}

// NewVerifier constructs the SDK adapter with both required callbacks.
func NewVerifier[P any](config Config[P]) (*Verifier[P], error) {
	if config.Validate == nil {
		return nil, errors.New("construct verifier: validation callback is required")
	}
	if config.ReportFailure == nil {
		return nil, errors.New("construct verifier: failure reporter is required")
	}
	return &Verifier[P]{validate: config.Validate, reportFailure: config.ReportFailure}, nil
}

// Verify implements auth.TokenVerifier. Operational errors are reported once and
// return a fixed public error. A nil or zero receiver returns that error without
// reporting, because no reporter was configured. The SDK owns expiration checks.
func (verifier *Verifier[P]) Verify(ctx context.Context, credential string, request *http.Request) (*auth.TokenInfo, error) {
	if verifier == nil || verifier.validate == nil {
		return nil, errVerificationFailed
	}
	token, err := verifier.validate(ctx, credential, request)
	if errors.Is(err, ErrUnauthenticated) {
		return nil, auth.ErrInvalidToken
	}
	if err != nil {
		verifier.reportFailure(ctx, fmt.Errorf("verify application token: %w", err))
		return nil, errVerificationFailed
	}
	if token == nil || token.bindingID == "" || token.expiresAt.IsZero() {
		verifier.reportFailure(ctx, errors.New("verify application token: validator returned an unconstructed token"))
		return nil, errVerificationFailed
	}
	return &auth.TokenInfo{UserID: token.bindingID, Expiration: token.expiresAt, Scopes: slices.Clone(token.scopes), Extra: map[string]any{principalKey: principalValue[P]{value: token.principal}}}, nil
}
