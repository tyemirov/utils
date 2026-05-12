package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

type proxySelectionContextKey struct{}

var ErrProxyLeaseUnavailable = errors.New("crawler: proxy lease unavailable")
var ErrProxyLeaseCandidatesExhausted = errors.New("crawler: proxy lease candidates exhausted")

// ProxyRotationUserConfig describes one ordered proxy credential inside a
// provider.
type ProxyRotationUserConfig struct {
	Name string
	URL  string
}

// ProxyRotationProviderConfig describes one ordered provider and its proxy
// users.
type ProxyRotationProviderConfig struct {
	Name  string
	Users []ProxyRotationUserConfig
}

// ProxyLease identifies the provider/user/proxy tuple chosen for one request.
type ProxyLease struct {
	ProviderName string
	UserName     string
	ProxyURL     string
	Generation   uint64
}

// Valid reports whether the lease has a concrete proxy URL.
func (lease ProxyLease) Valid() bool {
	return strings.TrimSpace(lease.ProxyURL) != ""
}

// ProxySelection identifies the provider/user/proxy tuple chosen for one
// request.
type ProxySelection = ProxyLease

type proxySelectionIndex struct {
	provider int
	user     int
}

type proxyRotationUser struct {
	name   string
	raw    string
	parsed *url.URL
}

type proxyRotationProvider struct {
	name     string
	users    []proxyRotationUser
	nextUser int
}

type proxyLeaseSelectorOptions struct {
	circuitBreakerEnabled bool
	startProviderIndex    int
	hasStartProviderIndex bool
	logger                Logger
	now                   func() time.Time
}

// ProxyLeaseSelectorOption configures proxy lease selector runtime behavior.
type ProxyLeaseSelectorOption func(*proxyLeaseSelectorOptions)

// ProxyLeaseSelectorCircuitBreaker enables or disables cooldown tracking.
func ProxyLeaseSelectorCircuitBreaker(enabled bool) ProxyLeaseSelectorOption {
	return func(options *proxyLeaseSelectorOptions) {
		options.circuitBreakerEnabled = enabled
	}
}

// ProxyLeaseSelectorStartProvider sets the initial active provider index.
func ProxyLeaseSelectorStartProvider(index int) ProxyLeaseSelectorOption {
	return func(options *proxyLeaseSelectorOptions) {
		options.startProviderIndex = index
		options.hasStartProviderIndex = true
	}
}

// ProxyLeaseSelectorLogger sets the selector logger used for cooldown notices.
func ProxyLeaseSelectorLogger(logger Logger) ProxyLeaseSelectorOption {
	return func(options *proxyLeaseSelectorOptions) {
		options.logger = logger
	}
}

// ProxyLeaseSelectorClock injects the selector clock for deterministic tests.
func ProxyLeaseSelectorClock(now func() time.Time) ProxyLeaseSelectorOption {
	return func(options *proxyLeaseSelectorOptions) {
		options.now = now
	}
}

// ProxyLeaseSelector acquires proxy leases from ordered providers and keeps
// successful leases sticky until a failure advances rotation.
type ProxyLeaseSelector struct {
	mu               sync.Mutex
	providers        []proxyRotationProvider
	selectionByProxy map[string]proxySelectionIndex
	activeProvider   int
	generation       uint64
	reservations     map[string]int
	healthTracker    *proxyHealthTracker
}

// ProxyLeaseAttemptScope tracks failed proxy leases for one scrape, request
// batch, or other caller-defined operation.
type ProxyLeaseAttemptScope struct {
	mu           sync.Mutex
	failedLeases map[string]struct{}
}

// NewProxyLeaseSelector constructs a provider-aware lease selector.
func NewProxyLeaseSelector(configs []ProxyRotationProviderConfig) (*ProxyLeaseSelector, error) {
	return NewProxyLeaseSelectorWithOptions(configs)
}

// NewProxyLeaseSelectorWithOptions constructs a provider-aware lease selector
// with runtime options such as cooldowns and initial provider position.
func NewProxyLeaseSelectorWithOptions(configs []ProxyRotationProviderConfig, optionList ...ProxyLeaseSelectorOption) (*ProxyLeaseSelector, error) {
	options := proxyLeaseSelectorOptions{}
	for _, option := range optionList {
		if option != nil {
			option(&options)
		}
	}

	providers := make([]proxyRotationProvider, 0, len(configs))
	selectionByProxy := make(map[string]proxySelectionIndex)
	proxyURLs := make([]string, 0)
	for _, providerConfig := range configs {
		providerName := strings.TrimSpace(providerConfig.Name)
		if providerName == "" {
			return nil, fmt.Errorf("proxy lease selector: provider name must not be empty")
		}
		users := make([]proxyRotationUser, 0, len(providerConfig.Users))
		providerIndex := len(providers)
		for _, userConfig := range providerConfig.Users {
			userName := strings.TrimSpace(userConfig.Name)
			if userName == "" {
				return nil, fmt.Errorf("proxy lease selector: provider %q has user with empty name", providerName)
			}
			trimmedProxyURL := strings.TrimSpace(userConfig.URL)
			if trimmedProxyURL == "" {
				continue
			}
			parsedProxyURL, err := url.Parse(trimmedProxyURL)
			if err != nil {
				return nil, fmt.Errorf("proxy lease selector: invalid proxy URL %q: %w", trimmedProxyURL, err)
			}
			if _, found := selectionByProxy[trimmedProxyURL]; found {
				continue
			}
			userIndex := len(users)
			selectionByProxy[trimmedProxyURL] = proxySelectionIndex{
				provider: providerIndex,
				user:     userIndex,
			}
			users = append(users, proxyRotationUser{
				name:   userName,
				raw:    trimmedProxyURL,
				parsed: parsedProxyURL,
			})
			proxyURLs = append(proxyURLs, trimmedProxyURL)
		}
		if len(users) == 0 {
			continue
		}
		providers = append(providers, proxyRotationProvider{
			name:  providerName,
			users: users,
		})
	}
	if len(providers) == 0 {
		return nil, nil
	}

	activeProvider := 0
	if options.hasStartProviderIndex {
		activeProvider = normalizeProxyProviderIndex(options.startProviderIndex, len(providers))
	}

	var healthTracker *proxyHealthTracker
	if options.circuitBreakerEnabled {
		healthTracker = newProxyHealthTracker(proxyURLs, options.logger)
		if options.now != nil {
			healthTracker.now = options.now
		}
	}

	return &ProxyLeaseSelector{
		providers:        providers,
		selectionByProxy: selectionByProxy,
		activeProvider:   activeProvider,
		reservations:     map[string]int{},
		healthTracker:    healthTracker,
	}, nil
}

// NewProxyLeaseAttemptScope constructs an operation-scoped failed-lease
// tracker.
func NewProxyLeaseAttemptScope() *ProxyLeaseAttemptScope {
	return &ProxyLeaseAttemptScope{failedLeases: map[string]struct{}{}}
}

// Acquire reserves and returns the best current proxy lease.
func (selector *ProxyLeaseSelector) Acquire() ProxyLease {
	lease, _ := selector.acquire()
	return lease
}

// AcquireRequired reserves and returns a proxy lease or a typed unavailable
// error when no proxy is configured.
func (selector *ProxyLeaseSelector) AcquireRequired() (ProxyLease, error) {
	return selector.acquire()
}

// CandidateCount returns the number of configured proxy candidates.
func (selector *ProxyLeaseSelector) CandidateCount() int {
	if selector == nil {
		return 0
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	candidateCount := 0
	for _, provider := range selector.providers {
		candidateCount += len(provider.users)
	}
	return candidateCount
}

// AcquireForRequest reserves a lease and attaches it to the request context for
// HTTP and Colly callers.
func (selector *ProxyLeaseSelector) AcquireForRequest(request *http.Request) (ProxyLease, error) {
	if selection, found := SelectedProxySelection(request); found {
		return selection, nil
	}
	lease, err := selector.AcquireRequired()
	if err != nil {
		return ProxyLease{}, err
	}
	AttachProxySelection(request, lease)
	return lease, nil
}

// Select returns the currently active provider/user tuple as a Colly proxy
// function.
func (selector *ProxyLeaseSelector) Select(request *http.Request) (*url.URL, error) {
	lease, err := selector.AcquireForRequest(request)
	if err != nil {
		return nil, err
	}
	return lease.proxyURL()
}

// Release releases a previously acquired lease reservation without reporting
// success or failure.
func (selector *ProxyLeaseSelector) Release(lease ProxyLease) {
	if selector == nil || !lease.Valid() {
		return
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selector.releaseLocked(lease)
}

// ReportFailure releases a lease and rotates immediately to the next provider.
func (selector *ProxyLeaseSelector) ReportFailure(lease ProxyLease) {
	selector.reportFailure(lease, false)
}

// ReportCriticalFailure releases a lease and immediately cools the proxy.
func (selector *ProxyLeaseSelector) ReportCriticalFailure(lease ProxyLease) {
	selector.reportFailure(lease, true)
}

func (selector *ProxyLeaseSelector) reportFailure(lease ProxyLease, critical bool) {
	if selector == nil || !lease.Valid() {
		return
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selector.releaseLocked(lease)
	selectionIndex, found := selector.selectionByProxy[strings.TrimSpace(lease.ProxyURL)]
	if !found {
		return
	}
	if critical {
		selector.recordCriticalFailureLocked(lease.ProxyURL)
	} else {
		selector.recordFailureLocked(lease.ProxyURL)
	}
	if lease.Generation != selector.generation {
		return
	}

	provider := &selector.providers[selectionIndex.provider]
	activeUserIndex := provider.nextUser % len(provider.users)
	if selectionIndex.provider != selector.activeProvider || activeUserIndex != selectionIndex.user {
		return
	}

	provider.nextUser = (selectionIndex.user + 1) % len(provider.users)
	if len(selector.providers) > 1 {
		selector.activeProvider = (selectionIndex.provider + 1) % len(selector.providers)
	}
	selector.generation++
}

// ReportSuccess releases a lease and keeps its provider/user tuple sticky.
func (selector *ProxyLeaseSelector) ReportSuccess(lease ProxyLease) {
	if selector == nil || !lease.Valid() {
		return
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selector.releaseLocked(lease)
	selectionIndex, found := selector.selectionByProxy[strings.TrimSpace(lease.ProxyURL)]
	if !found || lease.Generation != selector.generation {
		return
	}
	selector.recordSuccessLocked(lease.ProxyURL)

	provider := &selector.providers[selectionIndex.provider]
	activeUserIndex := provider.nextUser % len(provider.users)
	if selectionIndex.provider != selector.activeProvider || activeUserIndex != selectionIndex.user {
		return
	}

	provider.nextUser = selectionIndex.user
	selector.activeProvider = selectionIndex.provider
}

// RecordFailure keeps string-only callers compatible.
func (selector *ProxyLeaseSelector) RecordFailure(proxyURL string) {
	selection, found := selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	selector.ReportFailure(selection)
}

// RecordSuccess keeps string-only callers compatible.
func (selector *ProxyLeaseSelector) RecordSuccess(proxyURL string) {
	selection, found := selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	selector.ReportSuccess(selection)
}

// RecordCriticalFailure keeps string-only callers compatible.
func (selector *ProxyLeaseSelector) RecordCriticalFailure(proxyURL string) {
	selection, found := selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	selector.ReportCriticalFailure(selection)
}

// IsAvailable reports whether a proxy is outside cooldown.
func (selector *ProxyLeaseSelector) IsAvailable(proxyURL string) bool {
	if selector == nil || strings.TrimSpace(proxyURL) == "" {
		return true
	}
	selector.mu.Lock()
	defer selector.mu.Unlock()
	return selector.isAvailableLocked(proxyURL)
}

// SelectionForProxyURL returns the current-generation selection metadata for a
// known proxy URL.
func (selector *ProxyLeaseSelector) SelectionForProxyURL(proxyURL string) (ProxySelection, bool) {
	if selector == nil {
		return ProxySelection{}, false
	}

	normalizedProxyURL := strings.TrimSpace(proxyURL)
	if normalizedProxyURL == "" {
		return ProxySelection{}, false
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selectionIndex, found := selector.selectionByProxy[normalizedProxyURL]
	if !found {
		return ProxySelection{}, false
	}

	provider := selector.providers[selectionIndex.provider]
	user := provider.users[selectionIndex.user]
	return ProxySelection{
		ProviderName: provider.name,
		UserName:     user.name,
		ProxyURL:     user.raw,
		Generation:   selector.generation,
	}, true
}

// AcquireRequired reserves the next lease that has not failed inside this
// operation scope.
func (scope *ProxyLeaseAttemptScope) AcquireRequired(selector *ProxyLeaseSelector) (ProxyLease, error) {
	candidateCount := selector.CandidateCount()
	maxSkippedLeases := candidateCount
	if maxSkippedLeases < 1 {
		maxSkippedLeases = 1
	}

	for skippedLeases := 0; skippedLeases < maxSkippedLeases; skippedLeases++ {
		lease, err := selector.AcquireRequired()
		if err != nil {
			return ProxyLease{}, err
		}
		if !scope.Failed(lease) {
			return lease, nil
		}
		selector.ReportFailure(lease)
	}
	return ProxyLease{}, ProxyLeaseCandidatesExhaustedError(candidateCount)
}

// Failed reports whether the lease already failed inside this operation scope.
func (scope *ProxyLeaseAttemptScope) Failed(lease ProxyLease) bool {
	if scope == nil || !lease.Valid() {
		return false
	}

	scope.mu.Lock()
	defer scope.mu.Unlock()

	_, failed := scope.failedLeases[proxyLeaseAttemptScopeKey(lease)]
	return failed
}

// ReportFailure records a lease failure inside this operation scope.
func (scope *ProxyLeaseAttemptScope) ReportFailure(lease ProxyLease) {
	if scope == nil || !lease.Valid() {
		return
	}

	scope.mu.Lock()
	defer scope.mu.Unlock()

	if scope.failedLeases == nil {
		scope.failedLeases = map[string]struct{}{}
	}
	scope.failedLeases[proxyLeaseAttemptScopeKey(lease)] = struct{}{}
}

// Exhausted reports whether every configured candidate has failed inside this
// operation scope.
func (scope *ProxyLeaseAttemptScope) Exhausted(candidateCount int) bool {
	if scope == nil || candidateCount < 1 {
		return false
	}

	scope.mu.Lock()
	defer scope.mu.Unlock()

	return len(scope.failedLeases) >= candidateCount
}

// ProxyLeaseCandidatesExhaustedError wraps ErrProxyLeaseCandidatesExhausted
// with the attempted candidate count.
func ProxyLeaseCandidatesExhaustedError(candidateCount int) error {
	return fmt.Errorf("%w: all %d configured proxy candidate(s) failed", ErrProxyLeaseCandidatesExhausted, candidateCount)
}

func proxyLeaseAttemptScopeKey(lease ProxyLease) string {
	return strings.Join([]string{
		strings.TrimSpace(lease.ProviderName),
		strings.TrimSpace(lease.UserName),
		strings.TrimSpace(lease.ProxyURL),
	}, "\x00")
}

func (selector *ProxyLeaseSelector) acquire() (ProxyLease, error) {
	if selector == nil {
		return ProxyLease{}, fmt.Errorf("%w: no proxy providers configured", ErrProxyLeaseUnavailable)
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	if len(selector.providers) == 0 {
		return ProxyLease{}, fmt.Errorf("%w: no proxy providers configured", ErrProxyLeaseUnavailable)
	}
	candidateCount := selector.candidateCountLocked()
	if candidateCount == 0 {
		return ProxyLease{}, fmt.Errorf("%w: no proxy providers configured", ErrProxyLeaseUnavailable)
	}
	lease := selector.acquireLocked()
	if !lease.Valid() {
		return ProxyLease{}, ProxyLeaseCandidatesExhaustedError(candidateCount)
	}
	selector.reservations[lease.ProxyURL]++
	return lease, nil
}

func (selector *ProxyLeaseSelector) acquireLocked() ProxyLease {
	if len(selector.providers) == 0 {
		return ProxyLease{}
	}
	if selector.activeProvider < 0 || selector.activeProvider >= len(selector.providers) {
		selector.activeProvider = 0
	}
	if lease, found := selector.firstUnreservedLeaseFromProviderLocked(selector.activeProvider); found {
		return lease
	}

	for providerOffset := 1; providerOffset < len(selector.providers); providerOffset++ {
		providerIndex := (selector.activeProvider + providerOffset) % len(selector.providers)
		if lease, found := selector.firstUnreservedLeaseFromProviderLocked(providerIndex); found {
			return lease
		}
	}

	return selector.leastReservedAvailableLeaseLocked()
}

func (selector *ProxyLeaseSelector) firstUnreservedLeaseFromProviderLocked(providerIndex int) (ProxyLease, bool) {
	if providerIndex < 0 || providerIndex >= len(selector.providers) {
		return ProxyLease{}, false
	}

	provider := selector.providers[providerIndex]
	if len(provider.users) == 0 {
		return ProxyLease{}, false
	}
	userIndex := provider.nextUser
	if userIndex < 0 || userIndex >= len(provider.users) {
		userIndex = 0
	}

	for userOffset := 0; userOffset < len(provider.users); userOffset++ {
		candidateUserIndex := (userIndex + userOffset) % len(provider.users)
		candidateUser := provider.users[candidateUserIndex]
		if selector.reservations[candidateUser.raw] > 0 {
			continue
		}
		if !selector.isAvailableLocked(candidateUser.raw) {
			continue
		}
		return selector.leaseForProviderUser(provider, candidateUser), true
	}
	return ProxyLease{}, false
}

func (selector *ProxyLeaseSelector) leastReservedAvailableLeaseLocked() ProxyLease {
	selectedReservationCount := 0
	var selectedLease ProxyLease

	for providerOffset := 0; providerOffset < len(selector.providers); providerOffset++ {
		providerIndex := (selector.activeProvider + providerOffset) % len(selector.providers)
		provider := selector.providers[providerIndex]
		if len(provider.users) == 0 {
			continue
		}
		userIndex := provider.nextUser
		if userIndex < 0 || userIndex >= len(provider.users) {
			userIndex = 0
		}
		for userOffset := 0; userOffset < len(provider.users); userOffset++ {
			candidateUserIndex := (userIndex + userOffset) % len(provider.users)
			candidateUser := provider.users[candidateUserIndex]
			if !selector.isAvailableLocked(candidateUser.raw) {
				continue
			}
			reservationCount := selector.reservations[candidateUser.raw]
			if !selectedLease.Valid() || reservationCount < selectedReservationCount {
				selectedLease = selector.leaseForProviderUser(provider, candidateUser)
				selectedReservationCount = reservationCount
			}
		}
	}

	return selectedLease
}

func (selector *ProxyLeaseSelector) leaseForProviderUser(provider proxyRotationProvider, user proxyRotationUser) ProxyLease {
	return ProxyLease{
		ProviderName: provider.name,
		UserName:     user.name,
		ProxyURL:     user.raw,
		Generation:   selector.generation,
	}
}

func (lease ProxyLease) proxyURL() (*url.URL, error) {
	parsedProxyURL, err := url.Parse(lease.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy lease selector: invalid selected proxy URL %q: %w", lease.ProxyURL, err)
	}
	return parsedProxyURL, nil
}

func (selector *ProxyLeaseSelector) candidateCountLocked() int {
	candidateCount := 0
	for _, provider := range selector.providers {
		candidateCount += len(provider.users)
	}
	return candidateCount
}

func (selector *ProxyLeaseSelector) isAvailableLocked(proxyURL string) bool {
	return selector.healthTracker == nil || selector.healthTracker.IsAvailable(proxyURL)
}

func (selector *ProxyLeaseSelector) recordSuccessLocked(proxyURL string) {
	if selector.healthTracker != nil {
		selector.healthTracker.RecordSuccess(proxyURL)
	}
}

func (selector *ProxyLeaseSelector) recordFailureLocked(proxyURL string) {
	if selector.healthTracker != nil {
		selector.healthTracker.RecordFailure(proxyURL)
	}
}

func (selector *ProxyLeaseSelector) recordCriticalFailureLocked(proxyURL string) {
	if selector.healthTracker != nil {
		selector.healthTracker.RecordCriticalFailure(proxyURL)
	}
}

func (selector *ProxyLeaseSelector) releaseLocked(lease ProxyLease) {
	reservations := selector.reservations[strings.TrimSpace(lease.ProxyURL)]
	switch {
	case reservations <= 1:
		delete(selector.reservations, strings.TrimSpace(lease.ProxyURL))
	default:
		selector.reservations[strings.TrimSpace(lease.ProxyURL)] = reservations - 1
	}
}

func normalizeProxyProviderIndex(index int, providerCount int) int {
	if providerCount <= 0 {
		return 0
	}
	return ((index % providerCount) + providerCount) % providerCount
}

// ProxyRotationSelector keeps one (provider, user) selection sticky until a
// proxy-related failure advances to the next provider. When the selector later
// returns to a failed provider, it uses that provider's next user.
type ProxyRotationSelector struct {
	leaseSelector *ProxyLeaseSelector
}

// NewProxyRotationSelector constructs a provider-aware proxy selector.
func NewProxyRotationSelector(configs []ProxyRotationProviderConfig) (*ProxyRotationSelector, error) {
	leaseSelector, err := NewProxyLeaseSelector(configs)
	if err != nil {
		return nil, err
	}
	if leaseSelector == nil {
		return nil, nil
	}
	return &ProxyRotationSelector{leaseSelector: leaseSelector}, nil
}

// Select returns the currently active provider/user tuple.
func (selector *ProxyRotationSelector) Select(request *http.Request) (*url.URL, error) {
	if selector == nil || selector.leaseSelector == nil {
		return nil, nil
	}
	proxyURL, err := selector.leaseSelector.Select(request)
	if errors.Is(err, ErrProxyLeaseUnavailable) {
		return nil, nil
	}
	return proxyURL, err
}

// RecordFailure keeps string-only callers compatible.
func (selector *ProxyRotationSelector) RecordFailure(proxyURL string) {
	if selector == nil || selector.leaseSelector == nil {
		return
	}
	selector.leaseSelector.RecordFailure(proxyURL)
}

// RecordSuccess keeps string-only callers compatible.
func (selector *ProxyRotationSelector) RecordSuccess(proxyURL string) {
	if selector == nil || selector.leaseSelector == nil {
		return
	}
	selector.leaseSelector.RecordSuccess(proxyURL)
}

// RecordProxyFailure rotates to the next provider and advances the failed
// provider's user cursor.
func (selector *ProxyRotationSelector) RecordProxyFailure(selection ProxySelection) {
	if selector == nil || selector.leaseSelector == nil {
		return
	}
	selector.leaseSelector.ReportFailure(selection)
}

// RecordProxyCriticalFailure cools the selected proxy immediately.
func (selector *ProxyRotationSelector) RecordProxyCriticalFailure(selection ProxySelection) {
	if selector == nil || selector.leaseSelector == nil {
		return
	}
	selector.leaseSelector.ReportCriticalFailure(selection)
}

// RecordProxySuccess makes the successful provider/user tuple sticky until the
// next accepted failure.
func (selector *ProxyRotationSelector) RecordProxySuccess(selection ProxySelection) {
	if selector == nil || selector.leaseSelector == nil {
		return
	}
	selector.leaseSelector.ReportSuccess(selection)
}

// SelectionForProxyURL returns the current-generation selection metadata for a
// known proxy URL.
func (selector *ProxyRotationSelector) SelectionForProxyURL(proxyURL string) (ProxySelection, bool) {
	if selector == nil || selector.leaseSelector == nil {
		return ProxySelection{}, false
	}
	return selector.leaseSelector.SelectionForProxyURL(proxyURL)
}

// AttachProxySelection records a selection on a request context.
func AttachProxySelection(request *http.Request, selection ProxySelection) {
	if request == nil || strings.TrimSpace(selection.ProxyURL) == "" {
		return
	}

	ctx := context.WithValue(request.Context(), proxySelectionContextKey{}, selection)
	ctx = context.WithValue(ctx, colly.ProxyURLKey, selection.ProxyURL)
	*request = *request.WithContext(ctx)
}

// SelectedProxySelection reads a proxy selection previously attached to a
// request.
func SelectedProxySelection(request *http.Request) (ProxySelection, bool) {
	if request == nil {
		return ProxySelection{}, false
	}

	selection, ok := request.Context().Value(proxySelectionContextKey{}).(ProxySelection)
	if !ok || strings.TrimSpace(selection.ProxyURL) == "" {
		return ProxySelection{}, false
	}
	return selection, true
}
