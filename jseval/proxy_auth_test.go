package jseval

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

func TestProxyAuthEventHandlerAuthRequired(t *testing.T) {
	original := proxyAuthRunner
	defer func() { proxyAuthRunner = original }()

	var mu sync.Mutex
	var capturedActions []chromedp.Action

	proxyAuthRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		mu.Lock()
		capturedActions = append(capturedActions, actions...)
		mu.Unlock()
		return nil
	}

	handler := newProxyAuthEventHandler(context.Background(), "testuser", "testpass")

	handler(&fetch.EventAuthRequired{
		RequestID: fetch.RequestID("req_123"),
	})

	// Give the goroutine time to execute
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(capturedActions) == 0 {
		t.Fatal("expected auth action to be captured")
	}
}

func TestProxyAuthEventHandlerRequestPaused(t *testing.T) {
	original := proxyAuthRunner
	defer func() { proxyAuthRunner = original }()

	var mu sync.Mutex
	var callCount int

	proxyAuthRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	}

	handler := newProxyAuthEventHandler(context.Background(), "testuser", "testpass")

	handler(&fetch.EventRequestPaused{
		RequestID: fetch.RequestID("req_456"),
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount == 0 {
		t.Fatal("expected request paused action to be captured")
	}
}

func TestProxyAuthEventHandlerIgnoresUnknownEvents(t *testing.T) {
	original := proxyAuthRunner
	defer func() { proxyAuthRunner = original }()

	var callCount int
	proxyAuthRunner = func(ctx context.Context, actions ...chromedp.Action) error {
		callCount++
		return nil
	}

	handler := newProxyAuthEventHandler(context.Background(), "testuser", "testpass")

	// Send an unrelated event type
	handler("unrelated event")

	time.Sleep(50 * time.Millisecond)

	if callCount != 0 {
		t.Fatalf("expected no actions for unknown event, got %d", callCount)
	}
}
