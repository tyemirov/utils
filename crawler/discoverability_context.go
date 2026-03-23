package crawler

import "context"

type discoverabilityProbeContextKey struct{}

// WithDiscoverabilityProbeEnabled sets whether discoverability probing should run for this crawl context.
func WithDiscoverabilityProbeEnabled(ctx context.Context, enabled bool) context.Context {
	resolvedContext := ctx
	if resolvedContext == nil {
		resolvedContext = context.Background()
	}
	return context.WithValue(resolvedContext, discoverabilityProbeContextKey{}, enabled)
}

func discoverabilityProbeEnabledFromContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	enabled, hasValue := ctx.Value(discoverabilityProbeContextKey{}).(bool)
	if !hasValue {
		return true
	}
	return enabled
}
