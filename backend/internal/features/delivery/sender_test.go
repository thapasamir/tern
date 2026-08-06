package delivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// The servers below listen on 127.0.0.1, so tests that want the request to go through use
// allowLocal. The one test that checks the guard uses the production setting instead.
func newLocalSender(timeout time.Duration) *HTTPSender { return NewHTTPSender(true, timeout) }

func TestSend_PostsTheBodyAndHeaders(t *testing.T) {
	var gotMethod, gotContentType, gotUserAgent, gotCustom string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotContentType, gotUserAgent = r.Method, r.Header.Get("Content-Type"), r.Header.Get("User-Agent")
		gotCustom = r.Header.Get("Webhook-Id")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "thanks")
	}))
	defer srv.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{
		URL:    srv.URL,
		Header: http.Header{"Webhook-Id": {"evt_1"}},
		Body:   []byte(`{"a":1}`),
	})

	if res.Err != nil || res.StatusCode != http.StatusAccepted || res.Body != "thanks" {
		t.Fatalf("result = %+v, want 202 with body \"thanks\"", res)
	}
	if gotMethod != http.MethodPost || string(gotBody) != `{"a":1}` {
		t.Errorf("server saw %s %q", gotMethod, gotBody)
	}
	if gotContentType != "application/json" || gotUserAgent != userAgent || gotCustom != "evt_1" {
		t.Errorf("headers: content-type=%q user-agent=%q webhook-id=%q", gotContentType, gotUserAgent, gotCustom)
	}
	if res.Duration <= 0 {
		t.Error("duration was not measured")
	}
}

func TestSend_ErrorStatusesAreResultsNotErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "kaput", http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{URL: srv.URL})
	if res.Err != nil || res.StatusCode != 500 || !strings.Contains(res.Body, "kaput") {
		t.Fatalf("result = %+v, want status 500 with the body and no error", res)
	}
}

func TestSend_CapsHowMuchOfTheResponseItReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 10*maxResponseBytes))
	}))
	defer srv.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{URL: srv.URL})
	if res.Err != nil || len(res.Body) != maxResponseBytes {
		t.Fatalf("body is %d bytes (err %v), want exactly the %d-byte cap", len(res.Body), res.Err, maxResponseBytes)
	}
}

func TestSend_SanitisesBinaryResponsesForStorage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{'o', 'k', 0x00, 0xff, 0xfe, '!'})
	}))
	defer srv.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{URL: srv.URL})
	if !utf8.ValidString(res.Body) || strings.ContainsRune(res.Body, 0) {
		t.Fatalf("body %q is not safe for a Postgres text column (invalid UTF-8 or NUL)", res.Body)
	}
	if !strings.HasPrefix(res.Body, "ok") || !strings.HasSuffix(res.Body, "!") {
		t.Fatalf("body %q lost its readable parts", res.Body)
	}
}

func TestSend_DoesNotFollowRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetHits.Add(1) }))
	defer target.Close()
	redirector := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	defer redirector.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{URL: redirector.URL})
	if res.Err != nil || res.StatusCode != http.StatusFound {
		t.Fatalf("result = %+v, want the 302 itself as the outcome", res)
	}
	if targetHits.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestSend_TimesOutOnSlowEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	start := time.Now()
	res := newLocalSender(100*time.Millisecond).Send(context.Background(), SendRequest{URL: srv.URL})
	if res.Err == nil || res.StatusCode != 0 {
		t.Fatalf("result = %+v, want a timeout error and no status", res)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v; the 100ms timeout was not enforced", elapsed)
	}
}

func TestSend_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	res := newLocalSender(10*time.Second).Send(ctx, SendRequest{URL: srv.URL})
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", res.Err)
	}
}

func TestSend_UnreachableAndInvalidDestinations(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there any more

	for name, target := range map[string]string{
		"connection refused": url,
		"unparseable url":    "http://%zz",
		"unsupported scheme": "gopher://example.com",
	} {
		t.Run(name, func(t *testing.T) {
			res := newLocalSender(2*time.Second).Send(context.Background(), SendRequest{URL: target})
			if res.Err == nil || res.StatusCode != 0 {
				t.Fatalf("result = %+v, want an error and no status", res)
			}
		})
	}
}

// This is the test that matters for security: with the production setting, a request to a
// loopback address must be refused when the connection is made, and nothing may reach the server.
func TestSend_RefusesPrivateAddressesAtConnectTime(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close() // listens on 127.0.0.1

	res := NewHTTPSender(false, 5*time.Second).Send(context.Background(), SendRequest{URL: srv.URL})
	if !errors.Is(res.Err, errBlockedAddress) {
		t.Fatalf("error = %v, want errBlockedAddress", res.Err)
	}
	if hits.Load() != 0 {
		t.Fatal("the request reached a loopback server despite the guard")
	}
}

func TestSend_BlocksPrivateAddressesEvenWhenReachedThroughAHostname(t *testing.T) {
	// "localhost" passes any check done on the URL string, but resolves to a loopback address.
	// Only a check on the resolved address (the dialer hook) can stop this.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	hostnameURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	res := NewHTTPSender(false, 5*time.Second).Send(context.Background(), SendRequest{URL: hostnameURL})
	if !errors.Is(res.Err, errBlockedAddress) || hits.Load() != 0 {
		t.Fatalf("error = %v, hits = %d; a hostname resolving to loopback must be refused", res.Err, hits.Load())
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent", "", 0},
		{"seconds", "120", 2 * time.Minute},
		{"seconds with spaces", "  30 ", 30 * time.Second},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"http date in the future", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{"http date in the past", now.Add(-time.Hour).Format(http.TimeFormat), 0},
		{"garbage", "soon", 0},
		{"a huge number is capped instead of overflowing", "999999999999999", maxParsedRetryAfter},
		{"a date far in the future is capped", now.Add(10 * maxParsedRetryAfter).Format(http.TimeFormat), maxParsedRetryAfter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseRetryAfter(c.value, now); got != c.want {
				t.Fatalf("parseRetryAfter(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestSend_ReportsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	res := newLocalSender(5*time.Second).Send(context.Background(), SendRequest{URL: srv.URL})
	if res.Err != nil || res.StatusCode != http.StatusTooManyRequests || res.RetryAfter != 42*time.Second {
		t.Fatalf("result = %+v, want a 429 with RetryAfter of 42s", res)
	}
}
