//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/api"
	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/dispatch"
	"github.com/samirthapa/tern/internal/features/delivery/retry"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
	"github.com/samirthapa/tern/internal/features/endpoint"
	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/queue"
	"github.com/samirthapa/tern/internal/shared/secretbox"
	"github.com/samirthapa/tern/internal/shared/testutil"
	"github.com/samirthapa/tern/internal/shared/urlguard"
	"github.com/samirthapa/tern/internal/worker"
)

const adminToken = "integration-admin-token-0123456789"

// stack is a running API plus a running worker, wired the way the two binaries wire them.
type stack struct {
	t       *testing.T
	url     string // the API's base URL
	secrets *secretbox.Box
}

// startStack starts an API server and a worker loop, both stopped when the test ends.
// retrySchedule is the delay after each failed attempt; nil means failures are final. Each tweak
// may change the configuration the API starts with.
func startStack(t *testing.T, retrySchedule []time.Duration, tweaks ...func(*config.Config)) *stack {
	t.Helper()
	secrets, err := secretbox.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", secretbox.KeyBytes))))
	if err != nil {
		t.Fatal(err)
	}
	// Rate limits are off unless a test turns one on: every test signs up from the same address.
	cfg := config.Config{AdminToken: adminToken, AllowLocalEndpoints: true, EventsPerSecond: 1000}
	for _, tweak := range tweaks {
		tweak(&cfg)
	}

	srv := httptest.NewServer(api.NewRouter(api.Deps{
		Pool: pool, Redis: rdb, Config: cfg, Log: testutil.QuietLogger(), Secrets: secrets,
	}))
	t.Cleanup(srv.Close)

	jobs := queue.New(rdb, delivery.QueueName)
	repo := delivery.NewRepository(pool, secrets)
	endpoints := endpoint.NewService(endpoint.NewRepository(pool, secrets), urlguard.NewValidator(true))
	processor := dispatch.NewProcessor(repo, sender.New(true, 2*time.Second), endpoints, dispatch.ProcessorConfig{
		Retry: retry.NewPolicy(retrySchedule), AutoDisableAfter: 20,
	})
	runner, err := worker.NewRunner(jobs, processor, worker.Options{
		Concurrency: 4, PollInterval: 20 * time.Millisecond, JobTimeout: 5 * time.Second, Lease: 15 * time.Second,
	}, testutil.QuietLogger())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { runner.Run(ctx) })
	// The sweeper hands over deliveries whose queue job was never created.
	deliveries := delivery.NewService(repo, jobs)
	wg.Go(func() {
		worker.Every(ctx, testutil.QuietLogger(), 200*time.Millisecond, "sweep", deliveries.EnqueueOrphans)
	})
	t.Cleanup(func() { cancel(); wg.Wait() })

	return &stack{t: t, url: srv.URL, secrets: secrets}
}

// response is a decoded API response.
type response struct {
	Status int
	Body   map[string]any
}

// get digs a value out of a nested response: get("api_key", "key").
func (r response) get(path ...string) any {
	var cur any = r.Body
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func (r response) str(path ...string) string {
	s, _ := r.get(path...).(string)
	return s
}

// call performs one API request. body may be nil.
func (s *stack) call(method, path, token string, body any, headers ...string) response {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.url+path, reader)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode}
	_ = json.Unmarshal(raw, &out.Body) // an empty or non-object body leaves Body nil
	return out
}

// newTenant creates a tenant and returns its API key. Every test uses its own, so tests never
// see each other's data even though they share the databases.
func (s *stack) newTenant(name string) string {
	s.t.Helper()
	r := s.call(http.MethodPost, "/admin/tenants", adminToken, map[string]any{"name": name})
	if r.Status != http.StatusCreated {
		s.t.Fatalf("create tenant: status %d, body %v", r.Status, r.Body)
	}
	return r.str("api_key", "key")
}

// newEndpoint registers an endpoint and returns its id and signing secret.
func (s *stack) newEndpoint(key, url string) (id, secret string) {
	s.t.Helper()
	r := s.call(http.MethodPost, "/v1/endpoints", key, map[string]any{"url": url})
	if r.Status != http.StatusCreated {
		s.t.Fatalf("create endpoint: status %d, body %v", r.Status, r.Body)
	}
	return r.str("id"), r.str("secret")
}

// sendEvent posts an event and returns its id.
func (s *stack) sendEvent(key, eventType string) string {
	s.t.Helper()
	r := s.call(http.MethodPost, "/v1/events", key, map[string]any{"type": eventType, "payload": map[string]any{"n": 1}})
	if r.Status != http.StatusAccepted {
		s.t.Fatalf("send event: status %d, body %v", r.Status, r.Body)
	}
	return r.str("id")
}

// eventually polls cond until it holds or the time runs out.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// deliveriesOf returns the deliveries the API lists for an event.
func (s *stack) deliveriesOf(key, eventID string) []any {
	s.t.Helper()
	r := s.call(http.MethodGet, "/v1/events/"+eventID+"/deliveries?limit=100", key, nil)
	data, _ := r.get("data").([]any)
	return data
}

// settled reports whether the event has want deliveries and none of them is pending.
func (s *stack) settled(key, eventID string, want int) bool {
	ds := s.deliveriesOf(key, eventID)
	if len(ds) != want {
		return false
	}
	for _, d := range ds {
		if d.(map[string]any)["status"] == "pending" {
			return false
		}
	}
	return true
}
