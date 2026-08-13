// Package metrics exposes Prometheus metrics. It is the only package that knows about Prometheus:
// the rest of the code reports what happened through small observer interfaces that it defines
// itself (httpx.HTTPObserver, dispatch.Observer, worker.Observer), which the types here implement.
// That keeps the business logic free of any monitoring library and easy to test with plain fakes.
//
// All metric names start with "tern_". Labels only ever take values from small fixed sets (route
// patterns, not raw paths), so the number of time series stays bounded.
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "tern"

// Registry holds a process's metrics and serves them.
type Registry struct {
	reg *prometheus.Registry
}

// NewRegistry returns a Registry that already reports Go runtime and process metrics.
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return &Registry{reg: reg}
}

// Handler serves the metrics in the Prometheus text format. A collector that fails does not take
// the rest of the page down with it.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
}

// Server returns an HTTP server for a process that has no API of its own: it serves /metrics
// and a /healthz liveness check. Bind it to an internal address, since it is not authenticated.
func (r *Registry) Server(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", r.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

func (r *Registry) register(cs ...prometheus.Collector) { r.reg.MustRegister(cs...) }

// ---- HTTP ------------------------------------------------------------------------------------

// HTTP measures the API. It implements httpx.HTTPObserver and the rate limiter's observer.
type HTTP struct {
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	rateLimited *prometheus.CounterVec
}

// NewHTTP registers the API metrics on r.
func NewHTTP(r *Registry) *HTTP {
	m := &HTTP{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "http_requests_total",
			Help: "HTTP requests handled, by method, route pattern and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds",
			Help:    "HTTP request latency, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "rate_limited_requests_total",
			Help: "Requests refused with 429 because a tenant exceeded its rate limit, by limit.",
		}, []string{"scope"}),
	}
	r.register(m.requests, m.duration, m.rateLimited)
	return m
}

// RequestFinished records one handled request.
func (m *HTTP) RequestFinished(method, route string, status int, d time.Duration) {
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(d.Seconds())
}

// RateLimited records one request refused by a rate limit.
func (m *HTTP) RateLimited(scope string) { m.rateLimited.WithLabelValues(scope).Inc() }

// ---- Worker ----------------------------------------------------------------------------------

// Worker measures the delivery worker. It implements dispatch.Observer and worker.Observer.
type Worker struct {
	attempts          *prometheus.CounterVec
	responseTime      prometheus.Histogram
	throttled         prometheus.Counter
	endpointsDisabled *prometheus.CounterVec
	jobs              *prometheus.CounterVec
	inFlight          prometheus.Gauge
}

// NewWorker registers the worker metrics on r.
func NewWorker(r *Registry) *Worker {
	m := &Worker{
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "delivery_attempts_total",
			Help: "Delivery attempts recorded, by outcome: succeeded, retrying, failed or skipped.",
		}, []string{"outcome"}),
		responseTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "delivery_response_seconds",
			Help:    "How long customers' servers took to answer, for attempts that made a request.",
			Buckets: []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),
		throttled: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "deliveries_throttled_total",
			Help: "Deliveries held back because their endpoint was receiving too many.",
		}),
		endpointsDisabled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "endpoints_disabled_total",
			Help: "Endpoints disabled automatically, by reason: auto (too many failures) or gone (HTTP 410).",
		}, []string{"reason"}),
		jobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_jobs_total",
			Help: "Jobs the worker finished, by result: completed, retry_scheduled, failed or panic.",
		}, []string{"result"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "worker_jobs_in_flight",
			Help: "Jobs the worker is running right now.",
		}),
	}
	r.register(m.attempts, m.responseTime, m.throttled, m.endpointsDisabled, m.jobs, m.inFlight)
	return m
}

// AttemptFinished records a delivery attempt that was written to the delivery log. A response
// time of zero means no request was made, so it is kept out of the latency histogram.
func (m *Worker) AttemptFinished(outcome string, responseTime time.Duration) {
	m.attempts.WithLabelValues(outcome).Inc()
	if responseTime > 0 {
		m.responseTime.Observe(responseTime.Seconds())
	}
}

// Throttled records a delivery held back to protect its endpoint.
func (m *Worker) Throttled() { m.throttled.Inc() }

// EndpointDisabled records an endpoint that was disabled automatically.
func (m *Worker) EndpointDisabled(reason string) { m.endpointsDisabled.WithLabelValues(reason).Inc() }

// JobStarted records that the worker began a job.
func (m *Worker) JobStarted() { m.inFlight.Inc() }

// JobFinished records that the worker finished a job with the given result.
func (m *Worker) JobFinished(result string) {
	m.inFlight.Dec()
	m.jobs.WithLabelValues(result).Inc()
}

// ---- Queue -----------------------------------------------------------------------------------

// QueueDepth is a snapshot of the job queue.
type QueueDepth struct {
	Due          int64         // waiting jobs ready to run now
	Scheduled    int64         // waiting jobs set to run later
	Processing   int64         // jobs a worker has claimed
	OldestDueAge time.Duration // how long the longest-waiting due job has been due
}

// RegisterQueueDepth exposes the queue's depth. fn is called on every scrape, so the numbers are
// always current; if it fails, the queue metrics are left out of that scrape and the failure is
// logged, which an alert on absent metrics will notice.
func (r *Registry) RegisterQueueDepth(fn func(ctx context.Context) (QueueDepth, error), log *slog.Logger) {
	r.register(&queueCollector{
		fn:  fn,
		log: log,
		jobs: prometheus.NewDesc(namespace+"_queue_jobs",
			"Jobs in the delivery queue, by state: due, scheduled or processing.", []string{"state"}, nil),
		lag: prometheus.NewDesc(namespace+"_queue_oldest_due_job_age_seconds",
			"How long the longest-waiting due job has been waiting. Near zero when workers keep up.", nil, nil),
	})
}

type queueCollector struct {
	fn   func(ctx context.Context) (QueueDepth, error)
	log  *slog.Logger
	jobs *prometheus.Desc
	lag  *prometheus.Desc
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.jobs
	ch <- c.lag
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d, err := c.fn(ctx)
	if err != nil {
		c.log.Error("reading the queue depth for metrics failed", "error", err)
		return
	}
	for state, n := range map[string]int64{"due": d.Due, "scheduled": d.Scheduled, "processing": d.Processing} {
		ch <- prometheus.MustNewConstMetric(c.jobs, prometheus.GaugeValue, float64(n), state)
	}
	ch <- prometheus.MustNewConstMetric(c.lag, prometheus.GaugeValue, d.OldestDueAge.Seconds())
}
