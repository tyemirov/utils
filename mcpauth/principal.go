package mcpauth

import (
	"errors"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const principalKey = "github.com/tyemirov/utils/mcpauth/principal"

type principalValue[P any] struct{ value P }

// Principal obtains the application's principal from converted SDK token data.
// It returns an error when data is absent or uses a different principal type.
func Principal[P any](info *auth.TokenInfo) (P, error) {
	var zero P
	if info == nil {
		return zero, errors.New("read token principal: token information is absent")
	}
	principal, ok := info.Extra[principalKey].(principalValue[P])
	if !ok {
		return zero, errors.New("read token principal: principal is absent or has another type")
	}
	return principal.value, nil
}
