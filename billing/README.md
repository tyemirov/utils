# Billing: Stripe and Paddle Payments

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/billing`

Use `billing` for subscriptions, one-time credit packs, customer portals, and
provider webhooks. The package supplies Stripe and Paddle providers, a shared
service, webhook processors, and a GORM subscription repository.

## Start

The executable [service and webhook examples](examples_test.go) show the public API.
Run them from the repository root:

```sh
make test-unit UNIT_PACKAGES='./billing -run Example -v'
```

`ExampleService` reads subscription state and creates a checkout session.
`ExampleWebhookHandler` sends a local HTTP webhook through the handler.
The examples use local provider implementations and need no provider credentials.

## Connect an Application

1. Define plans and packs with `PlanCatalogItem` and `PackCatalogItem`.
2. Construct `NewStripeProvider` or `NewPaddleProvider` with the provider settings and required dependencies.
3. Call `Migrate` for the subscription schema and construct `NewSubscriptionStateRepository`.
4. Construct `NewSubscriptionStatusWebhookProcessor` to apply subscription events.
5. If events grant application credits, add `NewCreditsWebhookProcessor` with the application credit adapter.
6. Combine processors with `NewWebhookProcessorChain` and expose `NewWebhookHandler` through the application router.
7. Construct `NewService` for summaries, checkout, customer portals, synchronization, and checkout reconciliation.

The application controls authentication, route payloads, the checkout interface,
and its credit ledger. Provider credentials, price identifiers, webhook secrets,
and a database connection come from application configuration.

## Public API

| Task | Entry point |
| --- | --- |
| Select a provider | `NewStripeProvider`, `NewPaddleProvider` |
| Read subscription state and create checkout sessions | `Service` |
| Verify and process provider events | `NewWebhookHandler`, `WebhookProcessor` |
| Store subscription state | `Migrate`, `NewSubscriptionStateRepository` |
| Combine state and credit processors | `NewWebhookProcessorChain` |

See [package documentation](doc.go) and [provider contracts](provider.go) for the integration boundaries.

## Consumer Source Example

PoodleScanner imports this package in `internal/billing/service.go`,
`internal/billing/provider_factory.go`, and `internal/handlers/billing.go`.
These source examples were checked on 2026-09-10. They show source usage.
Provider access and deployed behavior need separate verification.

## Validation

```sh
make test-unit UNIT_PACKAGES=./billing
```

The [architecture document](../ARCHITECTURE.md#criteria-for-separate-modules)
records the criteria for an separate billing module.
