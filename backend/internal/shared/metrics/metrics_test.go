package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/shared/testutil"
)

// scrape returns what /metrics currently serves.
func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics answered %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

func wantLine(t *testing.T, page, line string) {
	t.Helper()
	if !strings.Contains(page, line+"\n") {
		t.Fatalf("metrics page is missing the line %q\n--- page ---\n%s", line, page)
	}
}

func TestHandler_ServesRuntimeMetricsByDefault(t *testing.T) {
	page := scrape(t, NewRegistry())
	if !strings.Contains(page, "go_goroutines") || !strings.Contains(page, "process_cpu_seconds_total") {
		t.Fatal("the Go runtime and process metrics should be there without any setup")
	}
}

func TestHTTP_CountsRequestsByRouteAndStatus(t *testing.T) {
	r := NewRegistry()
	m := NewHTTP(r)
	m.RequestFinished("POST", "/v1/events", 202, 12*time.Millisecond)
	m.RequestFinished("POST", "/v1/events", 202, 30*time.Millisecond)
	m.RequestFinished("POST", "/v1/events", 429, time.Millisecond)
	m.RateLimited("events")

	page := scrape(t, r)
	wantLine(t, page, `tern_http_requests_total{method="POST",route="/v1/events",status="202"} 2`)
	wantLine(t, page, `tern_http_requests_total{method="POST",route="/v1/events",status="429"} 1`)
	wantLine(t, page, `tern_http_request_duration_seconds_count{method="POST",route="/v1/events"} 3`)
	wantLine(t, page, `tern_rate_limited_requests_total{scope="events"} 1`)
}

func TestWorker_RecordsAttemptsThrottlingDisablingAndJobs(t *testing.T) {
	r := NewRegistry()
	m := NewWorker(r)

	m.AttemptFinished("succeeded", 120*time.Millisecond)
	m.AttemptFinished("succeeded", 80*time.Millisecond)
	m.AttemptFinished("retrying", 2*time.Second)
	m.AttemptFinished("skipped", 0) // no request was made: counted as an attempt, not as a latency
	m.Throttled()
	m.Throttled()
	m.EndpointDisabled("auto")
	m.EndpointDisabled("gone")
	m.JobStarted()
	m.JobStarted()
	m.JobFinished("completed")

	page := scrape(t, r)
	wantLine(t, page, `tern_delivery_attempts_total{outcome="succeeded"} 2`)
	wantLine(t, page, `tern_delivery_attempts_total{outcome="retrying"} 1`)
	wantLine(t, page, `tern_delivery_attempts_total{outcome="skipped"} 1`)
	wantLine(t, page, `tern_delivery_response_seconds_count 3`) // the skipped attempt is not a latency sample
	wantLine(t, page, `tern_deliveries_throttled_total 2`)
	wantLine(t, page, `tern_endpoints_disabled_total{reason="auto"} 1`)
	wantLine(t, page, `tern_endpoints_disabled_total{reason="gone"} 1`)
	wantLine(t, page, `tern_worker_jobs_total{result="completed"} 1`)
	wantLine(t, page, `tern_worker_jobs_in_flight 1`) // two started, one finished
}

func TestQueueDepth_IsReadFreshOnEveryScrape(t *testing.T) {
	r := NewRegistry()
	depth := QueueDepth{Due: 3, Scheduled: 7, Processing: 2, OldestDueAge: 4500 * time.Millisecond}
	r.RegisterQueueDepth(func(context.Context) (QueueDepth, error) { return depth, nil }, testutil.QuietLogger())

	page := scrape(t, r)
	wantLine(t, page, `tern_queue_jobs{state="due"} 3`)
	wantLine(t, page, `tern_queue_jobs{state="scheduled"} 7`)
	wantLine(t, page, `tern_queue_jobs{state="processing"} 2`)
	wantLine(t, page, `tern_queue_oldest_due_job_age_seconds 4.5`)

	depth = QueueDepth{Due: 0}
	wantLine(t, scrape(t, r), `tern_queue_jobs{state="due"} 0`)
}

func TestQueueDepth_AFailureLeavesTheQueueMetricsOutButKeepsTheRestWorking(t *testing.T) {
	r := NewRegistry()
	NewHTTP(r).RequestFinished("GET", "/healthz", 200, time.Millisecond)
	r.RegisterQueueDepth(func(context.Context) (QueueDepth, error) { return QueueDepth{}, errors.New("redis down") }, testutil.QuietLogger())

	page := scrape(t, r)
	if strings.Contains(page, "tern_queue_jobs") {
		t.Fatal("a failed read must not report made-up zeros")
	}
	wantLine(t, page, `tern_http_requests_total{method="GET",route="/healthz",status="200"} 1`)
}

func TestServer_ServesMetricsAndLiveness(t *testing.T) {
	srv := NewRegistry().Server(":0")
	for path, want := range map[string]int{"/metrics": 200, "/healthz": 200, "/nope": 404} {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
	if srv.ReadHeaderTimeout == 0 {
		t.Error("the server needs a header timeout")
	}
}

func TestRegistries_AreIndependent(t *testing.T) {
	a, b := NewRegistry(), NewRegistry()
	NewHTTP(a).RateLimited("events")
	NewHTTP(b) // registering the same metrics on another registry must not collide
	if strings.Contains(scrape(t, b), `scope="events"`) {
		t.Fatal("metrics leaked between registries")
	}
}
