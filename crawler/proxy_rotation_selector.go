package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gocolly/colly/v2"
)

type proxySelectionContextKey struct{}

var ErrProxyLeaseUnavailable = errors.New("crawler: proxy lease unavailable")

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

// ProxyLeaseSelector acquires proxy leases from ordered providers and keeps
// successful leases sticky until a failure advances rotation.
type ProxyLeaseSelector struct {
	mu               sync.Mutex
	providers        []proxyRotationProvider
	selectionByProxy map[string]proxySelectionIndex
	activeProvider   int
	generation       uint64
	reservations     map[string]int
}

// NewProxyLeaseSelector constructs a provider-aware lease selector.
func NewProxyLeaseSelector(configs []ProxyRotationProviderConfig) (*ProxyLeaseSelector, error) {
	providers := make([]proxyRotationProvider, 0, len(configs))
	selectionByProxy := make(map[string]proxySelectionIndex)
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
	return &ProxyLeaseSelector{
		providers:        providers,
		selectionByProxy: selectionByProxy,
		reservations:     map[string]int{},
	}, nil
}

// Acquire reserves and returns the best current proxy lease.
func (selector *ProxyLeaseSelector) Acquire() ProxyLease {
	if selector == nil {
		return ProxyLease{}
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	lease := selector.acquireLocked()
	if lease.Valid() {
		selector.reservations[lease.ProxyURL]++
	}
	return lease
}

// AcquireRequired reserves and returns a proxy lease or a typed unavailable
// error when no proxy is configured.
func (selector *ProxyLeaseSelector) AcquireRequired() (ProxyLease, error) {
	lease := selector.Acquire()
	if !lease.Valid() {
		return ProxyLease{}, fmt.Errorf("%w: no proxy providers configured", ErrProxyLeaseUnavailable)
	}
	return lease, nil
}

// AcquireForRequest reserves a lease and attaches it to the request context for
// HTTP and Colly callers.
func (selector *ProxyLeaseSelector) AcquireForRequest(request *http.Request) (ProxyLease, error) {
	lease, err := selector.AcquireRequired()
	if err != nil {
		return ProxyLease{}, err
	}
	AttachProxySelection(request, lease)
	return lease, nil
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

	return selector.leastReservedLeaseLocked()
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
		return selector.leaseForProviderUser(provider, candidateUser), true
	}
	return ProxyLease{}, false
}

func (selector *ProxyLeaseSelector) leastReservedLeaseLocked() ProxyLease {
	fallbackLease := ProxyLease{}
	fallbackReservations := 0
	fallbackSet := false
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
			reservations := selector.reservations[candidateUser.raw]
			if fallbackSet && reservations >= fallbackReservations {
				continue
			}
			fallbackLease = selector.leaseForProviderUser(provider, candidateUser)
			fallbackReservations = reservations
			fallbackSet = true
		}
	}
	return fallbackLease
}

func (selector *ProxyLeaseSelector) leaseForProviderUser(provider proxyRotationProvider, user proxyRotationUser) ProxyLease {
	return ProxyLease{
		ProviderName: provider.name,
		UserName:     user.name,
		ProxyURL:     user.raw,
		Generation:   selector.generation,
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

	lease := selector.leaseSelector.Acquire()
	if !lease.Valid() {
		return nil, nil
	}
	AttachProxySelection(request, lease)
	parsedProxyURL, err := url.Parse(lease.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy rotation selector: invalid selected proxy URL %q: %w", lease.ProxyURL, err)
	}
	return parsedProxyURL, nil
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
