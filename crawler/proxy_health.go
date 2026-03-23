package crawler

import (
	"sync"
	"time"
)

const (
	defaultProxyFailureThreshold = 5
	defaultProxyCooldownBase     = 30 * time.Second
	defaultProxyCooldownMax      = 10 * time.Minute
	proxyCooldownMaxExponent     = 5
)

// NewProxyHealthTracker creates a circuit-breaker health tracker for proxies.
func NewProxyHealthTracker(values []string, logger Logger) ProxyHealth {
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

func (t *proxyHealthTracker) IsAvailable(proxy string) bool {
	if t == nil || proxy == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(proxy)
	if state.cooldownUntil.IsZero() {
		return true
	}
	if t.now().After(state.cooldownUntil) {
		state.cooldownUntil = time.Time{}
		state.consecutiveFailures = 0
		return true
	}
	return false
}

func (t *proxyHealthTracker) RecordSuccess(proxy string) {
	if t == nil || proxy == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(proxy)
	state.consecutiveFailures = 0
	state.cooldownUntil = time.Time{}
}

func (t *proxyHealthTracker) RecordFailure(proxy string) {
	t.recordFailure(proxy, false)
}

func (t *proxyHealthTracker) RecordCriticalFailure(proxy string) {
	t.recordFailure(proxy, true)
}

func (t *proxyHealthTracker) recordFailure(proxy string, immediate bool) {
	if t == nil || proxy == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(proxy)
	if immediate {
		if state.consecutiveFailures < t.failureThreshold {
			state.consecutiveFailures = t.failureThreshold
		} else {
			state.consecutiveFailures++
		}
	} else {
		state.consecutiveFailures++
	}
	if state.consecutiveFailures < t.failureThreshold {
		return
	}
	cooldown := t.cooldownBase * time.Duration(1<<minInt(state.consecutiveFailures-t.failureThreshold, proxyCooldownMaxExponent))
	if cooldown > t.cooldownMax {
		cooldown = t.cooldownMax
	}
	state.cooldownUntil = t.now().Add(cooldown)
	if t.logger != nil {
		if immediate {
			t.logger.Warning("Proxy %s paused for %s after critical failure", SanitizeProxyURL(proxy), cooldown)
			return
		}
		t.logger.Warning("Proxy %s paused for %s after %d consecutive failures", SanitizeProxyURL(proxy), cooldown, state.consecutiveFailures)
	}
}

func (t *proxyHealthTracker) ensureState(key string) *proxyHealthState {
	if state, ok := t.states[key]; ok {
		return state
	}
	state := &proxyHealthState{}
	t.states[key] = state
	return state
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
