package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhookProcessorFuncProcessInvokesFunction(t *testing.T) {
	expectedErr := errors.New("processing failed")
	var called bool
	fn := WebhookProcessorFunc(func(ctx context.Context, event WebhookEvent) error {
		called = true
		return expectedErr
	})

	err := fn.Process(context.Background(), WebhookEvent{EventID: "evt_1"})
	require.True(t, called)
	require.ErrorIs(t, err, expectedErr)
}

func TestWebhookProcessorFuncProcessReturnsNil(t *testing.T) {
	fn := WebhookProcessorFunc(func(ctx context.Context, event WebhookEvent) error {
		return nil
	})

	err := fn.Process(context.Background(), WebhookEvent{})
	require.NoError(t, err)
}

func TestNoopWebhookProcessorReturnsNil(t *testing.T) {
	processor := noopWebhookProcessor{}
	err := processor.Process(context.Background(), WebhookEvent{EventID: "evt_1"})
	require.NoError(t, err)
}

func TestPackLabelForCodeReturnsEmptyForUnknownCode(t *testing.T) {
	require.Equal(t, "", PackLabelForCode("unknown_code"))
	require.Equal(t, "", PackLabelForCode(""))
}

func TestPackLabelForCodeReturnsKnownLabels(t *testing.T) {
	require.Equal(t, "Top-Up Pack", PackLabelForCode(PackCodeTopUp))
	require.Equal(t, "Bulk Top-Up Pack", PackLabelForCode(PackCodeBulkTopUp))
}

func TestNormalizePurchaseKindTrimsAndLowercases(t *testing.T) {
	require.Equal(t, "subscription", NormalizePurchaseKind("  Subscription  "))
	require.Equal(t, "top_up_pack", NormalizePurchaseKind(" TOP_UP_PACK "))
	require.Equal(t, "", NormalizePurchaseKind(""))
	require.Equal(t, "", NormalizePurchaseKind("   "))
}
