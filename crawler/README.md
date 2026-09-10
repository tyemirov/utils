# Crawler: Pages, Content Rules, and Proxy Selection

[Package catalog](../README.md#find-a-library)

Import: `github.com/tyemirov/utils/crawler`

Use `crawler` to request product pages, evaluate their content, and receive
structured results. The service supplies concurrency control, retries, proxy
selection, response handlers, and lifecycle hooks.

## Start with a Local Page

Save this example as `main.go` in your application module. Run `go run main.go`.
It starts a local HTTP server and prints `demo true`.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tyemirov/utils/crawler"
)

type titleRule struct{}

func (titleRule) ConfiguredVerifierCount() int { return 1 }

func (titleRule) Evaluate(_ string, document *goquery.Document) (crawler.RuleEvaluation, error) {
	passed := document.Find("title").Text() == "Demo"
	return crawler.RuleEvaluation{
		Passed:             passed,
		ConfiguredVerifier: 1,
		RuleResults: []crawler.RuleResult{{
			Description:         "Page title",
			Passed:              passed,
			VerificationResults: []crawler.VerificationResult{{Passed: passed}},
		}},
	}, nil
}

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		fmt.Fprint(response, "<html><head><title>Demo</title></head><body>Demo</body></html>")
	}))
	defer server.Close()
	address, err := url.Parse(server.URL)
	if err != nil {
		log.Fatal(err)
	}
	product, err := crawler.NewProduct("demo", "local", server.URL)
	if err != nil {
		log.Fatal(err)
	}
	results := make(chan *crawler.Result, 1)
	service, err := crawler.NewService(crawler.Config{
		PlatformID:    "local",
		Scraper:       crawler.ScraperConfig{Parallelism: 1, HTTPTimeout: 5 * time.Second},
		Platform:      crawler.PlatformConfig{AllowedDomains: []string{address.Hostname()}},
		RuleEvaluator: titleRule{},
	}, results)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Run(ctx, []crawler.Product{product}); err != nil {
		log.Fatal(err)
	}
	select {
	case result := <-results:
		fmt.Println(result.ProductID, result.Success)
	case <-ctx.Done():
		log.Fatal(ctx.Err())
	}
}
```

The example reserves channel capacity for its single result. For larger batches,
consume results while `Run` executes. The caller controls the results channel.
Individual page failures appear in results. Check those results as well as the
error from `Run`.

## Application Responsibilities

The application supplies product URLs, allowed domains, a `RuleEvaluator`, and
result storage. `WithResponseHandlers` adds content-specific processing.
`WithServiceHook` adds initialization, run, and cleanup operations.
See [configuration](config.go) and [interfaces](interfaces.go) for the complete contracts.

## Proxy Selection

`ProxyLeaseSelector` shares provider and user selection across requests.
`ProxyLeaseAttemptScope` excludes failed candidates within one operation.
`RetryDecision` distinguishes rotation-only decisions from critical proxy failures.

See [selector contracts](proxy_rotation_selector.go),
[attempt scope](proxy_rotation_selector.go), and [retry contracts](retry.go).
The [selector tests](proxy_rotation_selector_test.go) cover selection behavior.

For browser sessions, use [browsertransport](../browsertransport/README.md).
For standalone HTTP requests, use [httptransport](../httptransport/README.md).

## Consumer Source Example

PoodleScanner imports this package in `internal/crawlera/crawler_runtime.go`
and `pkg/crawlerext/enriched_service.go`. These source examples were checked
on 2026-09-10. They show source usage, with deployment verified separately.

## Validation

Run package tests from the repository root:

```sh
make test-unit UNIT_PACKAGES=./crawler
```
