package crawler

import (
	"sync"
	"time"
)

type proxyHealth interface {
	IsAvailable(proxy string) bool
	RecordSuccess(proxy string)
	RecordFailure(proxy string)
	RecordCriticalFailure(proxy string)
}

type proxyHealthTracker struct {
	logger           Logger
	mu               sync.Mutex
	states           map[string]*proxyHealthState
	failureThreshold int
	cooldownBase     time.Duration
	cooldownMax      time.Duration
	now              func() time.Time
}

type proxyHealthState struct {
	consecutiveFailures int
	cooldownUntil       time.Time
}

const (
	defaultProxyFailureThreshold = 5
	defaultProxyCooldownBase     = 30 * time.Second
	defaultProxyCooldownMax      = 10 * time.Minute
	proxyCooldownMaxExponent     = 5
)

func newProxyHealthTracker(values []string, logger Logger) *proxyHealthTracker {
	states := make(map[string]*proxyHealthState, len(values))
	for _, raw := range values {
		states[raw] = &proxyHealthState{}
	}
	return &proxyHealthTracker{
		logger:           logger,
		states:           states,
		failureThreshold: defaultProxyFailureThreshold,
		cooldownBase:     defaultProxyCooldownBase,
		cooldownMax:      defaultProxyCooldownMax,
		now:              time.Now,
	}
}

func (tracker *proxyHealthTracker) IsAvailable(proxy string) bool {
	if tracker == nil || proxy == "" {
		return true
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := tracker.ensureState(proxy)
	if state.cooldownUntil.IsZero() {
		return true
	}
	if tracker.now().After(state.cooldownUntil) {
		state.cooldownUntil = time.Time{}
		state.consecutiveFailures = 0
		return true
	}
	return false
}

func (tracker *proxyHealthTracker) RecordSuccess(proxy string) {
	if tracker == nil || proxy == "" {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := tracker.ensureState(proxy)
	state.consecutiveFailures = 0
	state.cooldownUntil = time.Time{}
}

func (tracker *proxyHealthTracker) RecordFailure(proxy string) {
	tracker.recordFailure(proxy, false)
}

func (tracker *proxyHealthTracker) RecordCriticalFailure(proxy string) {
	tracker.recordFailure(proxy, true)
}

func (tracker *proxyHealthTracker) recordFailure(proxy string, immediate bool) {
	if tracker == nil || proxy == "" {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	state := tracker.ensureState(proxy)
	if immediate {
		if state.consecutiveFailures < tracker.failureThreshold {
			state.consecutiveFailures = tracker.failureThreshold
		} else {
			state.consecutiveFailures++
		}
	} else {
		state.consecutiveFailures++
	}
	if state.consecutiveFailures < tracker.failureThreshold {
		return
	}
	cooldown := tracker.cooldownBase * time.Duration(1<<minInt(state.consecutiveFailures-tracker.failureThreshold, proxyCooldownMaxExponent))
	if cooldown > tracker.cooldownMax {
		cooldown = tracker.cooldownMax
	}
	state.cooldownUntil = tracker.now().Add(cooldown)
	if tracker.logger != nil {
		if immediate {
			tracker.logger.Warning("Proxy %s paused for %s after critical failure", describeProxyForLog(proxy), cooldown)
			return
		}
		tracker.logger.Warning(
			"Proxy %s paused for %s after %d consecutive failures",
			describeProxyForLog(proxy),
			cooldown,
			state.consecutiveFailures,
		)
	}
}

func (tracker *proxyHealthTracker) ensureState(key string) *proxyHealthState {
	if state, ok := tracker.states[key]; ok {
		return state
	}
	state := &proxyHealthState{}
	tracker.states[key] = state
	return state
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
