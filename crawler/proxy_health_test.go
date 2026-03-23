package crawler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyHealthTrackerQuarantinesFailures(t *testing.T) {
	tracker := newProxyHealthTracker([]string{"http://proxy-one:8080"}, nil)
	tracker.now = func() time.Time { return time.Unix(0, 0) }

	for i := 0; i < defaultProxyFailureThreshold; i++ {
		tracker.RecordFailure("http://proxy-one:8080")
	}

	require.False(t, tracker.IsAvailable("http://proxy-one:8080"))

	tracker.now = func() time.Time { return time.Unix(0, 0).Add(defaultProxyCooldownBase * 2) }
	require.True(t, tracker.IsAvailable("http://proxy-one:8080"))
}

func TestProxyHealthTrackerResetsOnSuccess(t *testing.T) {
	tracker := newProxyHealthTracker([]string{"http://proxy-two:8080"}, nil)

	tracker.RecordFailure("http://proxy-two:8080")
	tracker.RecordFailure("http://proxy-two:8080")
	tracker.RecordSuccess("http://proxy-two:8080")

	require.True(t, tracker.IsAvailable("http://proxy-two:8080"))
}

func TestProxyHealthTrackerQuarantinesCriticalFailureImmediately(t *testing.T) {
	tracker := newProxyHealthTracker([]string{"http://proxy-three:8080"}, nil)
	tracker.now = func() time.Time { return time.Unix(0, 0) }

	tracker.RecordCriticalFailure("http://proxy-three:8080")

	require.False(t, tracker.IsAvailable("http://proxy-three:8080"))
}
