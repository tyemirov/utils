package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gocolly/colly/v2"
)

type proxySelectionContextKey struct{}

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

// ProxySelection identifies the provider/user/proxy tuple chosen for one
// request.
type ProxySelection struct {
	ProviderName string
	UserName     string
	ProxyURL     string
	Generation   uint64
}

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

// ProxyRotationSelector keeps one (provider, user) selection sticky until a
// proxy-related failure advances to the next provider. When the selector later
// returns to a failed provider, it uses that provider's next user.
type ProxyRotationSelector struct {
	mu               sync.Mutex
	providers        []proxyRotationProvider
	selectionByProxy map[string]proxySelectionIndex
	activeProvider   int
	generation       uint64
}

// NewProxyRotationSelector constructs a provider-aware proxy selector.
func NewProxyRotationSelector(configs []ProxyRotationProviderConfig) (*ProxyRotationSelector, error) {
	providers := make([]proxyRotationProvider, 0, len(configs))
	selectionByProxy := make(map[string]proxySelectionIndex)
	for _, cfg := range configs {
		providerName := strings.TrimSpace(cfg.Name)
		if providerName == "" {
			return nil, fmt.Errorf("proxy provider selector: provider name must not be empty")
		}
		users := make([]proxyRotationUser, 0, len(cfg.Users))
		for _, rawUser := range cfg.Users {
			userName := strings.TrimSpace(rawUser.Name)
			if userName == "" {
				return nil, fmt.Errorf("proxy provider selector: provider %q has user with empty name", providerName)
			}
			trimmedURL := strings.TrimSpace(rawUser.URL)
			if trimmedURL == "" {
				continue
			}
			parsedURL, err := url.Parse(trimmedURL)
			if err != nil {
				return nil, fmt.Errorf("proxy provider selector: invalid proxy URL %q: %w", trimmedURL, err)
			}
			if _, found := selectionByProxy[trimmedURL]; found {
				continue
			}
			userIndex := len(users)
			selectionByProxy[trimmedURL] = proxySelectionIndex{
				provider: len(providers),
				user:     userIndex,
			}
			users = append(users, proxyRotationUser{
				name:   userName,
				raw:    trimmedURL,
				parsed: parsedURL,
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
	return &ProxyRotationSelector{
		providers:        providers,
		selectionByProxy: selectionByProxy,
	}, nil
}

// Select returns the currently active provider/user tuple.
func (selector *ProxyRotationSelector) Select(request *http.Request) (*url.URL, error) {
	if selector == nil {
		return nil, nil
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	if len(selector.providers) == 0 {
		return nil, nil
	}
	activeProvider := &selector.providers[selector.activeProvider]
	if len(activeProvider.users) == 0 {
		return nil, nil
	}
	activeUserIndex := activeProvider.nextUser % len(activeProvider.users)
	activeUser := activeProvider.users[activeUserIndex]
	selection := ProxySelection{
		ProviderName: activeProvider.name,
		UserName:     activeUser.name,
		ProxyURL:     activeUser.raw,
		Generation:   selector.generation,
	}
	AttachProxySelection(request, selection)
	return activeUser.parsed, nil
}

// RecordFailure keeps string-only callers compatible.
func (selector *ProxyRotationSelector) RecordFailure(proxyURL string) {
	selection, found := selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	selector.RecordProxyFailure(selection)
}

// RecordSuccess keeps string-only callers compatible.
func (selector *ProxyRotationSelector) RecordSuccess(proxyURL string) {
	selection, found := selector.SelectionForProxyURL(proxyURL)
	if !found {
		return
	}
	selector.RecordProxySuccess(selection)
}

// RecordProxyFailure rotates to the next provider and advances the failed
// provider's user cursor.
func (selector *ProxyRotationSelector) RecordProxyFailure(selection ProxySelection) {
	if selector == nil {
		return
	}

	normalizedProxyURL := strings.TrimSpace(selection.ProxyURL)
	if normalizedProxyURL == "" {
		return
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selectionIndex, found := selector.selectionByProxy[normalizedProxyURL]
	if !found || selection.Generation != selector.generation {
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

// RecordProxySuccess makes the successful provider/user tuple sticky until the
// next accepted failure.
func (selector *ProxyRotationSelector) RecordProxySuccess(selection ProxySelection) {
	if selector == nil {
		return
	}

	normalizedProxyURL := strings.TrimSpace(selection.ProxyURL)
	if normalizedProxyURL == "" {
		return
	}

	selector.mu.Lock()
	defer selector.mu.Unlock()

	selectionIndex, found := selector.selectionByProxy[normalizedProxyURL]
	if !found || selection.Generation != selector.generation {
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

// SelectionForProxyURL returns the current-generation selection metadata for a
// known proxy URL.
func (selector *ProxyRotationSelector) SelectionForProxyURL(proxyURL string) (ProxySelection, bool) {
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
