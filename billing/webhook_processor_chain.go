package billing

import "context"

type webhookProcessorChain struct {
	processors []WebhookProcessor
}

// NewWebhookProcessorChain combines multiple processors into a single
// WebhookProcessor that executes them in order.
func NewWebhookProcessorChain(processors ...WebhookProcessor) WebhookProcessor {
	filteredProcessors := make([]WebhookProcessor, 0, len(processors))
	for _, processor := range processors {
		if processor == nil {
			continue
		}
		filteredProcessors = append(filteredProcessors, processor)
	}
	if len(filteredProcessors) == 0 {
		return noopWebhookProcessor{}
	}
	return &webhookProcessorChain{
		processors: filteredProcessors,
	}
}

func (chain *webhookProcessorChain) Process(ctx context.Context, event WebhookEvent) error {
	for _, processor := range chain.processors {
		if processErr := processor.Process(ctx, event); processErr != nil {
			return processErr
		}
	}
	return nil
}
