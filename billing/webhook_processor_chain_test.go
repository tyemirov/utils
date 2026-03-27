package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type countingWebhookProcessor struct {
	calls int
	err   error
}

func (processor *countingWebhookProcessor) Process(_ context.Context, _ WebhookEvent) error {
	processor.calls++
	return processor.err
}

func TestWebhookProcessorChainInvokesProcessorsInOrder(t *testing.T) {
	firstProcessor := &countingWebhookProcessor{}
	secondProcessor := &countingWebhookProcessor{}
	chain := NewWebhookProcessorChain(firstProcessor, nil, secondProcessor)

	processErr := chain.Process(context.Background(), WebhookEvent{})
	require.NoError(t, processErr)
	require.Equal(t, 1, firstProcessor.calls)
	require.Equal(t, 1, secondProcessor.calls)
}

func TestWebhookProcessorChainStopsOnError(t *testing.T) {
	firstProcessor := &countingWebhookProcessor{
		err: errors.New("first failure"),
	}
	secondProcessor := &countingWebhookProcessor{}
	chain := NewWebhookProcessorChain(firstProcessor, secondProcessor)

	processErr := chain.Process(context.Background(), WebhookEvent{})
	require.EqualError(t, processErr, "first failure")
	require.Equal(t, 1, firstProcessor.calls)
	require.Equal(t, 0, secondProcessor.calls)
}

func TestWebhookProcessorChainFiltersNilProcessors(t *testing.T) {
	processor := &countingWebhookProcessor{}
	chain := NewWebhookProcessorChain(nil, processor, nil)

	processErr := chain.Process(context.Background(), WebhookEvent{})
	require.NoError(t, processErr)
	require.Equal(t, 1, processor.calls)
}

func TestWebhookProcessorChainReturnsNoopWhenAllNil(t *testing.T) {
	chain := NewWebhookProcessorChain(nil, nil, nil)

	processErr := chain.Process(context.Background(), WebhookEvent{})
	require.NoError(t, processErr)
}
