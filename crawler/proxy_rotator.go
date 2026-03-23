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

// NewProxyRotator creates a round-robin proxy rotation function.
func NewProxyRotator(rawProxies []string, tracker ProxyHealth, logger Logger) (colly.ProxyFunc, error) {
	if len(rawProxies) == 0 {
		return nil, fmt.Errorf("crawler: proxy list is empty")
	}
	parsed := make([]*url.URL, 0, len(rawProxies))
	for _, candidate := range rawProxies {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		endpoint, err := url.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("crawler: invalid proxy %q: %w", value, err)
		}
		parsed = append(parsed, endpoint)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("crawler: proxy list is empty")
	}
	rotator := &proxyRotator{proxies: parsed, tracker: tracker, logger: logger, position: -1}
	return rotator.nextProxy, nil
}

type proxyRotator struct {
	mu       sync.Mutex
	proxies  []*url.URL
	tracker  ProxyHealth
	logger   Logger
	position int
}

func (r *proxyRotator) nextProxy(request *http.Request) (*url.URL, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.proxies) == 0 {
		return nil, fmt.Errorf("crawler: proxy list is empty")
	}
	for attempts := 0; attempts < len(r.proxies); attempts++ {
		r.position = (r.position + 1) % len(r.proxies)
		candidate := r.proxies[r.position]
		if r.tracker == nil || r.tracker.IsAvailable(candidate.String()) {
			attachProxyURL(request, candidate.String())
			return candidate, nil
		}
	}
	candidate := r.proxies[r.position]
	if r.logger != nil {
		r.logger.Warning("All proxies unavailable; reusing %s", candidate.Host)
	}
	attachProxyURL(request, candidate.String())
	return candidate, nil
}

func attachProxyURL(request *http.Request, proxyURL string) {
	if request == nil || strings.TrimSpace(proxyURL) == "" {
		return
	}
	ctx := context.WithValue(request.Context(), colly.ProxyURLKey, proxyURL)
	*request = *request.WithContext(ctx)
}
