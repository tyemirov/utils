package crawler

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	defaultCrawlerDialKeepAlive         = 30 * time.Second
	defaultCrawlerExpectContinueTimeout = time.Second
	defaultCrawlerIdleConnTimeout       = 90 * time.Second
	defaultCrawlerMaxIdleConns          = 100
)

func newCrawlerHTTPTransport(insecureSkipVerify bool, requestTimeout time.Duration) *http.Transport {
	dialer := &net.Dialer{
		KeepAlive: defaultCrawlerDialKeepAlive,
	}
	if requestTimeout > 0 {
		dialer.Timeout = requestTimeout
	}

	dialContext := dialer.DialContext
	if requestTimeout > 0 {
		dialContext = newIdleTimeoutDialContext(dialer.DialContext, requestTimeout)
	}

	httpTransport := &http.Transport{
		DialContext:           dialContext,
		ExpectContinueTimeout: defaultCrawlerExpectContinueTimeout,
		ForceAttemptHTTP2:     false,
		IdleConnTimeout:       defaultCrawlerIdleConnTimeout,
		MaxIdleConns:          defaultCrawlerMaxIdleConns,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
		},
	}

	return httpTransport
}

func newIdleTimeoutDialContext(
	baseDialContext func(context.Context, string, string) (net.Conn, error),
	idleTimeout time.Duration,
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := baseDialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return newIdleTimeoutConn(conn, idleTimeout), nil
	}
}

func newIdleTimeoutConn(conn net.Conn, idleTimeout time.Duration) net.Conn {
	if conn == nil || idleTimeout <= 0 {
		return conn
	}
	return &idleTimeoutConn{
		Conn:        conn,
		idleTimeout: idleTimeout,
	}
}

type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func (conn *idleTimeoutConn) Read(p []byte) (int, error) {
	if err := conn.Conn.SetReadDeadline(time.Now().Add(conn.idleTimeout)); err != nil {
		return 0, err
	}
	return conn.Conn.Read(p)
}

func (conn *idleTimeoutConn) Write(p []byte) (int, error) {
	if err := conn.Conn.SetWriteDeadline(time.Now().Add(conn.idleTimeout)); err != nil {
		return 0, err
	}
	return conn.Conn.Write(p)
}
