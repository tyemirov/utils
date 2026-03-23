package crawler

import (
	"net/http"

	"github.com/gocolly/colly/v2"
)

// RequestConfigurator sets up headers and cookies on the collector.
type RequestConfigurator interface {
	Configure(collector *colly.Collector)
}

// NewRequestConfigurator creates a configurator from the crawler config.
func NewRequestConfigurator(cfg Config, logger Logger) RequestConfigurator {
	return &requestConfigurator{
		category:       cfg.Category,
		cookieDomains:  cfg.Platform.CookieDomains,
		cookieProvider: cfg.CookieProvider,
		headerProvider: ensureHeaders(cfg.Headers),
		logger:         logger,
	}
}

type requestConfigurator struct {
	category       string
	cookieDomains  []string
	cookieProvider CookieProvider
	headerProvider HeaderProvider
	logger         Logger
}

func (c *requestConfigurator) Configure(collector *colly.Collector) {
	if c.cookieProvider != nil {
		for _, domain := range c.cookieDomains {
			cookies := c.cookieProvider(domain)
			for _, cookie := range cookies {
				if err := collector.SetCookies("https://"+domain, []*http.Cookie{cookie}); err != nil {
					c.logger.Error("failed to set cookie for %s: %v", domain, err)
				}
			}
		}
	}
	collector.OnRequest(func(request *colly.Request) {
		c.headerProvider.Apply(c.category, request)
		if request.Ctx.Get(CtxInitialURLKey) == "" {
			request.Ctx.Put(CtxInitialURLKey, request.URL.String())
		}
	})
}
