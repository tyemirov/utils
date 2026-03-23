package crawler

import (
	"net/http"

	"github.com/gocolly/colly/v2"
)

// RequestConfigurator applies cookies and headers to outgoing requests.
type RequestConfigurator interface {
	Configure(collector *colly.Collector)
}

type requestConfigurator struct {
	platformID      string
	cookieDomains   []string
	cookieGenerator CookieGenerator
	headerProvider  RequestHeaderProvider
	logger          Logger
}

func newRequestConfigurator(cfg Config, logger Logger) RequestConfigurator {
	return &requestConfigurator{
		platformID:      cfg.PlatformID,
		cookieDomains:   cfg.Platform.CookieDomains,
		cookieGenerator: cfg.CookieGenerator,
		headerProvider:  ensureRequestHeaders(cfg.RequestHeaders),
		logger:          logger,
	}
}

func (configurator *requestConfigurator) Configure(collector *colly.Collector) {
	if configurator.cookieGenerator != nil {
		for _, domain := range configurator.cookieDomains {
			cookies := configurator.cookieGenerator(domain)
			for _, cookie := range cookies {
				_ = collector.SetCookies("https://"+domain, []*http.Cookie{cookie})
			}
		}
	}

	collector.OnRequest(func(request *colly.Request) {
		configurator.headerProvider.Apply(configurator.platformID, request)
		if request.Ctx.Get(ctxInitialURLKey) == "" {
			request.Ctx.Put(ctxInitialURLKey, request.URL.String())
		}
	})
}
