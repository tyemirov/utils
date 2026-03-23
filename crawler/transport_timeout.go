package crawler

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	defaultDialKeepAlive         = 30 * time.Second
	defaultExpectContinueTimeout = time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultMaxIdleConns          = 100
)

// NewHTTPTransport creates an HTTP transport with idle-timeout support.
func NewHTTPTransport(insecureSkipVerify bool, requestTimeout time.Duration) *http.Transport {
	dialer := &net.Dialer{KeepAlive: defaultDialKeepAlive}
	if requestTimeout > 0 {
		dialer.Timeout = requestTimeout
	}
	dialContext := dialer.DialContext
	if requestTimeout > 0 {
		dialContext = newIdleTimeoutDialContext(dialer.DialContext, requestTimeout)
	}
	return &http.Transport{
		DialContext:           dialContext,
		ExpectContinueTimeout: defaultExpectContinueTimeout,
		ForceAttemptHTTP2:     false,
		IdleConnTimeout:       defaultIdleConnTimeout,
		MaxIdleConns:          defaultMaxIdleConns,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureSkipVerify},
	}
}

func newIdleTimeoutDialContext(
	base func(context.Context, string, string) (net.Conn, error),
	idleTimeout time.Duration,
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := base(ctx, network, address)
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
	return &idleTimeoutConn{Conn: conn, idleTimeout: idleTimeout}
}

type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
