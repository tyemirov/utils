package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

const amazonSearchBaseURL = "https://www.amazon.com/s"
const defaultDiscoverabilityProbeTimeout = 20 * time.Second

// NewAmazonDiscoverabilityProber builds an HTTP-backed Amazon discoverability prober.
func NewAmazonDiscoverabilityProber(httpClient *http.Client, logger Logger) DiscoverabilityProber {
	resolvedClient := httpClient
	if resolvedClient == nil {
		resolvedClient = newDiscoverabilityHTTPClient(logger, nil, defaultDiscoverabilityProbeTimeout)
	}
	return &amazonDiscoverabilityProber{
		httpClient: resolvedClient,
		logger:     ensureLogger(logger),
	}
}

type amazonDiscoverabilityProber struct {
	httpClient     *http.Client
	logger         Logger
	platformID     string
	requestHeaders RequestHeaderProvider
}

func (prober *amazonDiscoverabilityProber) Probe(ctx context.Context, targetASIN string) (Discoverability, error) {
	normalizedTargetASIN := normalizeASIN(targetASIN)
	if normalizedTargetASIN == "" {
		return Discoverability{}, fmt.Errorf("crawler: discoverability target asin required")
	}
	searchURL := buildAmazonSearchURL(normalizedTargetASIN)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return Discoverability{SearchURL: searchURL}, fmt.Errorf("crawler: discoverability build request %s: %w", normalizedTargetASIN, err)
	}
	prober.applyRequestHeaders(request)

	response, err := prober.httpClient.Do(request)
	if err != nil {
		return Discoverability{SearchURL: searchURL}, fmt.Errorf("crawler: discoverability request %s: %w", normalizedTargetASIN, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return Discoverability{SearchURL: searchURL}, fmt.Errorf("crawler: discoverability read response %s: %w", normalizedTargetASIN, err)
	}
	if len(body) == 0 {
		return Discoverability{SearchURL: searchURL}, fmt.Errorf("crawler: discoverability empty response %s", normalizedTargetASIN)
	}

	discoverability, err := parseAmazonSearchDiscoverability(body, normalizedTargetASIN, searchURL)
	if err != nil {
		return Discoverability{SearchURL: searchURL}, err
	}

	return discoverability, nil
}

func (prober *amazonDiscoverabilityProber) bindNetwork(
	platformID string,
	transport http.RoundTripper,
	timeout time.Duration,
	requestHeaders RequestHeaderProvider,
) {
	if prober == nil {
		return
	}

	resolvedTimeout := timeout
	if resolvedTimeout <= 0 {
		resolvedTimeout = defaultDiscoverabilityProbeTimeout
	}

	prober.httpClient = newDiscoverabilityHTTPClient(prober.logger, transport, resolvedTimeout)
	prober.platformID = strings.TrimSpace(platformID)
	prober.requestHeaders = ensureRequestHeaders(requestHeaders)
}

func newDiscoverabilityHTTPClient(logger Logger, transport http.RoundTripper, timeout time.Duration) *http.Client {
	effectiveTransport := transport
	if effectiveTransport == nil {
		effectiveTransport = newPanicSafeTransport(newCrawlerHTTPTransport(false, timeout), logger)
	}
	return &http.Client{
		Transport: effectiveTransport,
	}
}

func (prober *amazonDiscoverabilityProber) applyRequestHeaders(request *http.Request) {
	if request == nil {
		return
	}

	headers := request.Header
	if headers == nil {
		headers = make(http.Header)
	}

	if prober.requestHeaders != nil {
		requestContext := colly.NewContext()
		if request.Context() != nil {
			requestContext.Put(ctxRunContextKey, request.Context())
		}
		collyRequest := &colly.Request{
			URL:     request.URL,
			Method:  request.Method,
			Headers: &headers,
			Ctx:     requestContext,
		}
		prober.requestHeaders.Apply(prober.platformID, collyRequest)
	}

	request.Header = headers
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Crawler/1.0)")
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}
	if request.Header.Get("Accept-Language") == "" {
		request.Header.Set("Accept-Language", "en-US,en;q=0.5")
	}
}

func parseAmazonSearchDiscoverability(htmlContent []byte, targetASIN string, searchURL string) (Discoverability, error) {
	normalizedTargetASIN := normalizeASIN(targetASIN)
	if normalizedTargetASIN == "" {
		return Discoverability{}, fmt.Errorf("crawler: discoverability target asin required")
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return Discoverability{}, fmt.Errorf("crawler: discoverability parse response %s: %w", normalizedTargetASIN, err)
	}
	return parseAmazonSearchDiscoverabilityDocument(document, normalizedTargetASIN, searchURL), nil
}

func parseAmazonSearchDiscoverabilityDocument(document *goquery.Document, targetASIN string, searchURL string) Discoverability {
	discoverability := Discoverability{
		Status:                     DiscoverabilityStatusNotFound,
		TargetOrganicRank:          0,
		FirstOrganicASIN:           "",
		SponsoredBeforeTargetCount: 0,
		SearchURL:                  strings.TrimSpace(searchURL),
	}

	if document == nil {
		return discoverability
	}
	if amazonSearchLooksBlocked(document) {
		discoverability.Status = DiscoverabilityStatusBlocked
		return discoverability
	}

	type amazonSearchResultRow struct {
		ASIN        string
		IsSponsored bool
	}

	rows := make([]amazonSearchResultRow, 0, 32)
	document.Find("[data-component-type='s-search-result']").Each(func(_ int, selection *goquery.Selection) {
		asin := normalizeASIN(selection.AttrOr("data-asin", ""))
		if asin == "" {
			return
		}
		rows = append(rows, amazonSearchResultRow{
			ASIN:        asin,
			IsSponsored: amazonSearchRowIsSponsored(selection),
		})
	})

	hasSponsoredRows := false
	hasOrganicRows := false
	targetFound := false
	organicRank := 0
	sponsoredBeforeTargetCount := 0
	firstOrganicASIN := ""
	targetOrganicRank := 0

	for _, row := range rows {
		if row.IsSponsored {
			hasSponsoredRows = true
			if !targetFound {
				sponsoredBeforeTargetCount++
			}
			continue
		}
		hasOrganicRows = true
		organicRank++
		if firstOrganicASIN == "" {
			firstOrganicASIN = row.ASIN
		}
		if row.ASIN == targetASIN {
			targetFound = true
			targetOrganicRank = organicRank
		}
	}

	discoverability.FirstOrganicASIN = firstOrganicASIN
	if targetFound {
		discoverability.TargetOrganicRank = targetOrganicRank
		discoverability.SponsoredBeforeTargetCount = sponsoredBeforeTargetCount
		if targetOrganicRank == 1 {
			discoverability.Status = DiscoverabilityStatusFirstOrganic
		} else {
			discoverability.Status = DiscoverabilityStatusOrganicNotFirst
		}
		return discoverability
	}

	discoverability.SponsoredBeforeTargetCount = 0
	if !hasOrganicRows && hasSponsoredRows {
		discoverability.Status = DiscoverabilityStatusSponsoredOnly
		return discoverability
	}
	discoverability.Status = DiscoverabilityStatusNotFound
	return discoverability
}

func amazonSearchRowIsSponsored(selection *goquery.Selection) bool {
	if selection == nil {
		return false
	}
	if selection.Find("[aria-label*='Sponsored'], [class*='s-sponsored-label'], [id*='s-sponsored-label']").Length() > 0 {
		return true
	}
	normalizedText := strings.ToLower(strings.Join(strings.Fields(selection.Text()), " "))
	return strings.Contains(normalizedText, "sponsored")
}

func amazonSearchLooksBlocked(document *goquery.Document) bool {
	if document == nil {
		return false
	}
	if document.Find("form[action*='validateCaptcha'], input#captchacharacters").Length() > 0 {
		return true
	}
	normalizedText := strings.ToLower(strings.Join(strings.Fields(document.Text()), " "))
	if strings.Contains(normalizedText, "type the characters you see in this image") {
		return true
	}
	if strings.Contains(normalizedText, "enter the characters you see below") {
		return true
	}
	if strings.Contains(normalizedText, "robot check") {
		return true
	}
	return false
}

func normalizeASIN(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func buildAmazonSearchURL(targetASIN string) string {
	query := url.Values{}
	query.Set("k", normalizeASIN(targetASIN))
	return amazonSearchBaseURL + "?" + query.Encode()
}
