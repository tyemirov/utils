package billing

import (
	"context"
	"errors"
)

// CreditGranter is the interface that consuming applications implement
// to bridge billing webhook grants to their credit/ledger system.
type CreditGranter interface {
	GrantBillingCredits(ctx context.Context, input CreditGrantInput) error
}

// CreditGrantInput contains the data needed to grant credits from a billing event.
type CreditGrantInput struct {
	UserEmail      string
	Credits        int64
	IdempotencyKey string
	Reason         string
	Reference      string
	Metadata       map[string]string
}

// ErrDuplicateGrant signals an idempotent duplicate — not a failure.
var ErrDuplicateGrant = errors.New("billing: duplicate grant")
