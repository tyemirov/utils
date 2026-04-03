package billing

import (
	"strings"
	"time"
)

func canonicalProviderSubscription(
	subscriptions []ProviderSubscription,
) (ProviderSubscription, bool) {
	canonicalIndex := canonicalProviderSubscriptionIndex(subscriptions)
	if canonicalIndex < 0 {
		return ProviderSubscription{}, false
	}
	return normalizeProviderSubscription(subscriptions[canonicalIndex]), true
}

func canonicalProviderSubscriptionIndex(subscriptions []ProviderSubscription) int {
	bestIndex := -1
	var bestSubscription ProviderSubscription
	for candidateIndex, candidateSubscription := range subscriptions {
		normalizedCandidate := normalizeProviderSubscription(candidateSubscription)
		if bestIndex < 0 || providerSubscriptionPreferred(normalizedCandidate, bestSubscription) {
			bestIndex = candidateIndex
			bestSubscription = normalizedCandidate
		}
	}
	return bestIndex
}

func activeProviderSubscriptionExists(subscriptions []ProviderSubscription) bool {
	for _, subscription := range subscriptions {
		if isProviderSubscriptionActive(subscription) {
			return true
		}
	}
	return false
}

func isProviderSubscriptionActive(subscription ProviderSubscription) bool {
	return normalizeProviderSubscription(subscription).Status == subscriptionStatusActive
}

func providerSubscriptionPreferred(
	leftSubscription ProviderSubscription,
	rightSubscription ProviderSubscription,
) bool {
	leftIsActive := isProviderSubscriptionActive(leftSubscription)
	rightIsActive := isProviderSubscriptionActive(rightSubscription)
	if leftIsActive != rightIsActive {
		return leftIsActive
	}
	leftOccurredAt := normalizeProviderSubscription(leftSubscription).OccurredAt
	rightOccurredAt := normalizeProviderSubscription(rightSubscription).OccurredAt
	if !leftOccurredAt.Equal(rightOccurredAt) {
		return leftOccurredAt.After(rightOccurredAt)
	}
	leftSubscriptionID := normalizeProviderSubscription(leftSubscription).SubscriptionID
	rightSubscriptionID := normalizeProviderSubscription(rightSubscription).SubscriptionID
	return leftSubscriptionID > rightSubscriptionID
}

func normalizeProviderSubscription(subscription ProviderSubscription) ProviderSubscription {
	return ProviderSubscription{
		SubscriptionID: strings.TrimSpace(subscription.SubscriptionID),
		PlanCode:       strings.ToLower(strings.TrimSpace(subscription.PlanCode)),
		Status:         strings.ToLower(strings.TrimSpace(subscription.Status)),
		ProviderStatus: strings.ToLower(strings.TrimSpace(subscription.ProviderStatus)),
		NextBillingAt:  normalizeProviderSubscriptionTimestamp(subscription.NextBillingAt),
		OccurredAt:     normalizeProviderSubscriptionTimestamp(subscription.OccurredAt),
	}
}

func normalizeProviderSubscriptionTimestamp(timestamp time.Time) time.Time {
	if timestamp.IsZero() {
		return time.Time{}
	}
	return timestamp.UTC()
}
