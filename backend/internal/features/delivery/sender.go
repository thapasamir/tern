package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/samirthapa/webhook-platform/internal/shared/urlguard"
)

const (
	userAgent        = "WebhookPlatform/1.0"
	maxResponseBytes = 4 * 1024
)

// errBlockedAddress is returned when a destination resolves to a private or internal address.
var errBlockedAddress = errors.New("destination address is not allowed")

// SendRequest is one HTTP POST to make.
type SendRequest struct {
	URL    string
	Header http.Header
	Body   []byte
}

// SendResult is what came back. A response with any status code, even 500, is a result with
// Err == nil; Err is only set when no response was obtained (DNS, connect, TLS, timeout...).
type SendResult struct {
	StatusCode int // 0 when no response was received
	// Body is the beginning of the response, sanitised to be valid UTF-8 without NUL bytes.
	Body     string
	Duration time.Duration
	Err      error
}

// Sender performs the HTTP request of a delivery attempt.
type Sender interface {
	Send(ctx context.Context, req SendRequest) SendResult
}

// HTTPSender is the production Sender. It differs from a default http.Client in the ways that
// matter for calling addresses chosen by other people:
//   - connections to private and internal addresses are refused when the connection is made, on
//     the address actually resolved, which defeats DNS rebinding (see package urlguard);
//   - redirects are not followed, because a redirect could point at an internal address;
//   - environment proxies are ignored, because they could be used to route around the guard;
//   - every phase of the request has a timeout and the response size we read is capped.
type HTTPSender struct {
	client *http.Client
}

// NewHTTPSender returns a Sender with the given overall per-request timeout.
// allowLocal (development only) lifts the private-address restriction.
func NewHTTPSender(allowLocal bool, timeout time.Duration) *HTTPSender {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: guardConnections(allowLocal),
	}
	transport := &http.Transport{
		Proxy:                 nil, // never honour HTTP_PROXY and friends
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
	}
	return &HTTPSender{client: &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // report the 3xx itself as the outcome
		},
	}}
}

// guardConnections returns a net.Dialer Control hook. It runs for every connection attempt with
// the resolved IP address, after DNS and immediately before connecting.
func guardConnections(allowLocal bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allowLocal {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		if urlguard.BlockedAddr(addr) {
			return errBlockedAddress
		}
		return nil
	}
}

func (s *HTTPSender) Send(ctx context.Context, r SendRequest) (res SendResult) {
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(r.Body))
	if err != nil {
		return SendResult{Err: err}
	}
	for name, values := range r.Header {
		req.Header[name] = values
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return SendResult{Err: err}
	}
	defer resp.Body.Close()

	// A read error mid-body does not change the outcome: we already have the status code.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return SendResult{StatusCode: resp.StatusCode, Body: sanitize(string(body))}
}

// sanitize makes arbitrary bytes from a remote server safe to store in a Postgres text column,
// which rejects invalid UTF-8 and NUL characters.
func sanitize(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "")
}
