// Command loadtest measures how fast Tern accepts events and delivers them.
//
// It creates a tenant, starts a fake customer endpoint inside this process, registers it several
// times, then posts events as fast as -concurrency allows. It reports how long the API took to
// answer, and how long each delivery took to arrive, measured from just before its event was
// posted to the moment the fake endpoint received it.
//
// Start the stack first (make up, make run, make worker) with the rate limits raised, or most
// requests will be answered 429:
//
//	RATE_LIMIT_EVENTS_PER_SECOND=100000 ALLOW_LOCAL_ENDPOINTS=true make run
//	ALLOW_LOCAL_ENDPOINTS=true make worker
//	make loadtest
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type options struct {
	baseURL     string
	adminToken  string
	events      int
	endpoints   int
	concurrency int
	listen      string
	timeout     time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.baseURL, "url", "http://localhost:8080", "base URL of the Tern API")
	flag.StringVar(&o.adminToken, "admin-token", os.Getenv("ADMIN_TOKEN"), "admin token (default: $ADMIN_TOKEN)")
	flag.IntVar(&o.events, "events", 2000, "number of events to send")
	flag.IntVar(&o.endpoints, "endpoints", 3, "number of endpoints each event fans out to")
	flag.IntVar(&o.concurrency, "concurrency", 32, "simultaneous requests to the API")
	flag.StringVar(&o.listen, "listen", "127.0.0.1:0", "address of the fake customer endpoint")
	flag.DurationVar(&o.timeout, "timeout", 2*time.Minute, "how long to wait for deliveries after the last event is sent")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.adminToken == "" {
		return errors.New("an admin token is required (-admin-token or $ADMIN_TOKEN)")
	}
	api := &client{base: o.baseURL, http: &http.Client{Timeout: 30 * time.Second}}

	sink, err := startSink(o.listen)
	if err != nil {
		return err
	}
	defer sink.close()

	key, err := api.createTenant(o.adminToken)
	if err != nil {
		return err
	}
	for range o.endpoints {
		if err := api.createEndpoint(key, sink.url); err != nil {
			return err
		}
	}
	want := o.events * o.endpoints
	fmt.Printf("sending %d events to %d endpoints each (%d deliveries), %d at a time\n", o.events, o.endpoints, want, o.concurrency)

	started := time.Now()
	post := sendAll(api, key, sink, o)
	sendTime := time.Since(started)

	accepted := post.accepted()
	if accepted == 0 {
		return fmt.Errorf("no event was accepted (%d rate limited, %d failed): raise RATE_LIMIT_EVENTS_PER_SECOND", post.limited.Load(), post.failed.Load())
	}
	expected := accepted * o.endpoints
	waitFor(sink, expected, o.timeout)
	total := time.Since(started)

	report(o, post, sink, sendTime, total, expected)
	return nil
}

// ---- sending ---------------------------------------------------------------------------------

type postResult struct {
	mu        sync.Mutex
	latencies []time.Duration
	limited   atomic.Int64 // answered 429
	failed    atomic.Int64 // any other failure
}

func (p *postResult) accepted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.latencies)
}

func sendAll(api *client, key string, sink *sink, o options) *postResult {
	res := &postResult{}
	work := make(chan struct{})
	var wg sync.WaitGroup
	for range o.concurrency {
		wg.Go(func() {
			for range work {
				sentAt := time.Now()
				id, status, err := api.sendEvent(key)
				latency := time.Since(sentAt)
				switch {
				case err != nil:
					res.failed.Add(1)
				case status == http.StatusTooManyRequests:
					res.limited.Add(1)
				case status != http.StatusAccepted:
					res.failed.Add(1)
				default:
					sink.expect(id, sentAt)
					res.mu.Lock()
					res.latencies = append(res.latencies, latency)
					res.mu.Unlock()
				}
			}
		})
	}
	for range o.events {
		work <- struct{}{}
	}
	close(work)
	wg.Wait()
	return res
}

// waitFor blocks until the sink has received want deliveries, or the timeout passes.
func waitFor(s *sink, want int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && s.received() < want {
		time.Sleep(50 * time.Millisecond)
	}
}

// ---- the fake customer endpoint --------------------------------------------------------------

// sink receives deliveries and records how long each took to arrive.
type sink struct {
	url string
	srv *http.Server

	sentAt sync.Map // event id -> time it was posted
	mu     sync.Mutex
	lags   []time.Duration
	hits   atomic.Int64
}

func startSink(addr string) (*sink, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("start the fake endpoint: %w", err)
	}
	s := &sink{url: "http://" + ln.Addr().String()}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 5 * time.Second}
	go s.srv.Serve(ln) //nolint:errcheck // always ErrServerClosed at shutdown
	return s, nil
}

func (s *sink) handle(w http.ResponseWriter, r *http.Request) {
	arrived := time.Now()
	_, _ = io.Copy(io.Discard, r.Body)
	s.hits.Add(1)
	// webhook-id carries the event id. An unknown id is a retry of a delivery already counted
	// or a stray request; it still counts as received but has no lag to record.
	if sent, ok := s.sentAt.Load(r.Header.Get("Webhook-Id")); ok {
		s.mu.Lock()
		s.lags = append(s.lags, arrived.Sub(sent.(time.Time)))
		s.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *sink) expect(eventID string, sentAt time.Time) { s.sentAt.Store(eventID, sentAt) }
func (s *sink) received() int                           { return int(s.hits.Load()) }
func (s *sink) close()                                  { _ = s.srv.Shutdown(context.Background()) }

// ---- the API ---------------------------------------------------------------------------------

type client struct {
	base string
	http *http.Client
}

func (c *client) do(method, path, token string, body any, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body) // let the connection be reused
	}
	return resp.StatusCode, nil
}

func (c *client) createTenant(adminToken string) (string, error) {
	var out struct {
		APIKey struct {
			Key string `json:"key"`
		} `json:"api_key"`
	}
	status, err := c.do(http.MethodPost, "/admin/tenants", adminToken, map[string]string{"name": "Load test"}, &out)
	if err != nil || status != http.StatusCreated {
		return "", fmt.Errorf("create tenant: status %d, err %v (is the API running, and is the admin token right?)", status, err)
	}
	return out.APIKey.Key, nil
}

func (c *client) createEndpoint(key, url string) error {
	status, err := c.do(http.MethodPost, "/v1/endpoints", key, map[string]string{"url": url}, nil)
	if err != nil || status != http.StatusCreated {
		return fmt.Errorf("create endpoint: status %d, err %v (the API needs ALLOW_LOCAL_ENDPOINTS=true)", status, err)
	}
	return nil
}

func (c *client) sendEvent(key string) (id string, status int, err error) {
	var out struct {
		ID string `json:"id"`
	}
	status, err = c.do(http.MethodPost, "/v1/events", key, map[string]any{"type": "load.test", "payload": map[string]int{"n": 1}}, &out)
	return out.ID, status, err
}

// ---- the report ------------------------------------------------------------------------------

func report(o options, post *postResult, s *sink, sendTime, total time.Duration, expected int) {
	got := s.received()
	fmt.Printf("\nevents      accepted %d, rate limited %d, failed %d\n", post.accepted(), post.limited.Load(), post.failed.Load())
	fmt.Printf("api         %.0f events/s, latency %s\n", float64(post.accepted())/sendTime.Seconds(), summary(post.latencies))
	fmt.Printf("deliveries  %d of %d arrived in %s (%.0f/s)\n", got, expected, total.Round(time.Millisecond), float64(got)/total.Seconds())
	fmt.Printf("lag         post to arrival: %s\n", summary(s.lags))
	if got < expected {
		fmt.Printf("\n%d deliveries had not arrived after waiting %s: the worker is slower than the load, or retries are pending\n", expected-got, o.timeout)
	}
}

// summary formats the p50, p95, p99 and max of a set of durations.
func summary(ds []time.Duration) string {
	if len(ds) == 0 {
		return "no samples"
	}
	sorted := slices.Clone(ds)
	slices.Sort(sorted)
	at := func(q float64) time.Duration {
		return sorted[int(q*float64(len(sorted)-1))].Round(100 * time.Microsecond)
	}
	return fmt.Sprintf("p50 %s  p95 %s  p99 %s  max %s", at(0.50), at(0.95), at(0.99), at(1))
}
