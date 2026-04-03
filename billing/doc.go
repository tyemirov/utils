// Package billing provides shared billing primitives for applications that sell
// subscriptions and one-off packs through Paddle or Stripe.
//
// The package is intentionally split into a few layers:
//
//   - provider implementations for Paddle and Stripe
//   - a Service for summary, checkout, portal, sync, and reconcile flows
//   - webhook processors for subscription-state updates and optional credit grants
//   - a GORM-backed subscription state repository
//
// A typical integration looks like:
//
//  1. Build a provider with NewPaddleProvider or NewStripeProvider using a
//     data-driven catalog of plans and packs.
//  2. Call Migrate and create a SubscriptionStateRepository.
//  3. Build a subscription-state webhook processor with
//     NewSubscriptionStatusWebhookProcessor.
//  4. Optionally add a credit-grant processor with NewCreditsWebhookProcessor
//     if billing events need to grant app-specific credits.
//  5. Combine processors with NewWebhookProcessorChain and expose
//     NewWebhookHandler.
//  6. Use Service from your authenticated application endpoints to serve
//     summaries, create checkout sessions, create portal sessions, refresh
//     state, and reconcile completed checkouts.
//
// The shared package stops at backend billing mechanics. Consumer applications
// still own authentication, HTTP payload shapes, checkout-launch UX, and any
// mapping from billing events to local ledgers or virtual-currency systems.
//
// See ExampleService and ExampleWebhookHandler for minimal end-to-end wiring.
package billing
