package mcpauth

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// VerifiedToken contains token data accepted by the application validator.
// Its zero value cannot authorize a request. Construct it with NewVerifiedToken.
type VerifiedToken[P any] struct {
	bindingID string
	expiresAt time.Time
	scopes    []string
	principal P
}

// NewVerifiedToken constructs validated token data. The binding identifies the
// authorized session relation, rather than the bearer credential or account.
// Scopes are copied. The application must keep the principal immutable.
func NewVerifiedToken[P any](bindingID string, expiresAt time.Time, scopes []string, principal P) (*VerifiedToken[P], error) {
	if strings.TrimSpace(bindingID) == "" {
		return nil, errors.New("construct verified token: session binding is required")
	}
	if expiresAt.IsZero() {
		return nil, errors.New("construct verified token: expiration is required")
	}
	return &VerifiedToken[P]{bindingID: bindingID, expiresAt: expiresAt, scopes: slices.Clone(scopes), principal: principal}, nil
}
