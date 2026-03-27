// Package billing provides a dual-provider (Paddle/Stripe) billing system
// with webhook processing, subscription management, and credit granting.
//
// Consuming applications implement the CreditGranter interface to bridge
// billing webhook grants to their own credit/ledger system.
package billing
