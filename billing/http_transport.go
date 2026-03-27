package billing

import (
	"net"
	"net/http"
	"time"
)

const (
	billingHTTPDialTimeout           = 30 * time.Second
	billingHTTPKeepAlive             = 30 * time.Second
	billingHTTPMaxIdleConns          = 100
	billingHTTPIdleConnTimeout       = 90 * time.Second
	billingHTTPTLSHandshakeTimeout   = 10 * time.Second
	billingHTTPExpectContinueTimeout = 1 * time.Second
	billingHTTPClientRequestTimeout  = 15 * time.Second
)

func newDirectBillingHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   billingHTTPDialTimeout,
			KeepAlive: billingHTTPKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          billingHTTPMaxIdleConns,
		IdleConnTimeout:       billingHTTPIdleConnTimeout,
		TLSHandshakeTimeout:   billingHTTPTLSHandshakeTimeout,
		ExpectContinueTimeout: billingHTTPExpectContinueTimeout,
	}
	return &http.Client{
		Timeout:   billingHTTPClientRequestTimeout,
		Transport: transport,
	}
}
