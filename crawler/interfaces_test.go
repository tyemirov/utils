package crawler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/require"
)

func TestNoopResponseHandlerHandleBinaryResponseReturnsFalse(t *testing.T) {
	t.Parallel()

	handler := NoopResponseHandler{}
	handled := handler.HandleBinaryResponse(&colly.Response{}, "PRODUCT-001", ".jpg")

	require.False(t, handled)
}

func TestNoopResponseHandlerBeforeEvaluationDoesNotPanic(t *testing.T) {
	t.Parallel()

	handler := NoopResponseHandler{}

	require.NotPanics(t, func() {
		handler.BeforeEvaluation(nil, nil)
	})
}

func TestNoopResponseHandlerAfterEvaluationDoesNotPanic(t *testing.T) {
	t.Parallel()

	handler := NoopResponseHandler{}

	require.NotPanics(t, func() {
		handler.AfterEvaluation(nil, nil, nil)
	})
}

func TestNoopServiceHookAfterInitDoesNotPanic(t *testing.T) {
	t.Parallel()

	hook := noopServiceHook{}

	require.NotPanics(t, func() {
		hook.AfterInit(nil, nil)
	})
}

func TestNoopServiceHookBeforeRunDoesNotPanic(t *testing.T) {
	t.Parallel()

	hook := noopServiceHook{}

	require.NotPanics(t, func() {
		hook.BeforeRun(context.Background())
	})
}

func TestNoopServiceHookAfterRunDoesNotPanic(t *testing.T) {
	t.Parallel()

	hook := noopServiceHook{}

	require.NotPanics(t, func() {
		hook.AfterRun()
	})
}

func TestWithResponseHandlersAppendsHandlersToService(t *testing.T) {
	t.Parallel()

	firstHandler := NoopResponseHandler{}
	secondHandler := NoopResponseHandler{}

	service := &Service{}
	option := WithResponseHandlers(firstHandler, secondHandler)
	option(service)

	require.Len(t, service.responseHandlers, 2)
}

func TestWithResponseHandlersAppendsToExistingHandlers(t *testing.T) {
	t.Parallel()

	existingHandler := NoopResponseHandler{}
	additionalHandler := NoopResponseHandler{}

	service := &Service{
		responseHandlers: []ResponseHandler{existingHandler},
	}
	option := WithResponseHandlers(additionalHandler)
	option(service)

	require.Len(t, service.responseHandlers, 2)
}

func TestWithServiceHookSetsHookOnService(t *testing.T) {
	t.Parallel()

	hook := &recordingServiceHook{}

	service := &Service{
		serviceHook: noopServiceHook{},
	}
	option := WithServiceHook(hook)
	option(service)

	require.Equal(t, hook, service.serviceHook)
}

func TestWithServiceHookIgnoresNilHook(t *testing.T) {
	t.Parallel()

	originalHook := noopServiceHook{}
	service := &Service{
		serviceHook: originalHook,
	}
	option := WithServiceHook(nil)
	option(service)

	require.Equal(t, originalHook, service.serviceHook)
}

type recordingServiceHook struct {
	afterInitCalled  bool
	beforeRunCalled  bool
	afterRunCalled   bool
	initCollector    *colly.Collector
	initTransport    http.RoundTripper
	beforeRunContext context.Context
}

func (hook *recordingServiceHook) AfterInit(collector *colly.Collector, transport http.RoundTripper) {
	hook.afterInitCalled = true
	hook.initCollector = collector
	hook.initTransport = transport
}

func (hook *recordingServiceHook) BeforeRun(ctx context.Context) {
	hook.beforeRunCalled = true
	hook.beforeRunContext = ctx
}

func (hook *recordingServiceHook) AfterRun() {
	hook.afterRunCalled = true
}
