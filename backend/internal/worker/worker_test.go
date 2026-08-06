package worker

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samirthapa/webhook-platform/internal/shared/testutil"
)

// fakeQueue hands out the jobs it holds, at most `limit` per claim, and records every call.
type fakeQueue struct {
	mu        sync.Mutex
	pending   []string
	acked     []string
	limits    []int // the limit of each Claim call
	claimErrs int   // fail this many Claim calls first
	retried   []retryCall
	retryErr  error
}

type retryCall struct {
	id string
	at time.Time
}

func (q *fakeQueue) Claim(_ context.Context, limit int, _ time.Duration) ([]string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.limits = append(q.limits, limit)
	if q.claimErrs > 0 {
		q.claimErrs--
		return nil, errors.New("redis: connection refused")
	}
	n := min(limit, len(q.pending))
	jobs := slices.Clone(q.pending[:n])
	q.pending = q.pending[n:]
	return jobs, nil
}

func (q *fakeQueue) Ack(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, id)
	return nil
}

func (q *fakeQueue) Retry(_ context.Context, id string, runAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retried = append(q.retried, retryCall{id, runAt})
	return q.retryErr
}

func (q *fakeQueue) retriedJobs() []retryCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.retried)
}

func (q *fakeQueue) ackedJobs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := slices.Clone(q.acked)
	slices.Sort(out)
	return out
}

func (q *fakeQueue) maxLimit() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Max(append([]int{0}, q.limits...))
}

// processorFunc adapts a plain function to Processor for jobs that finish (no retry).
type processorFunc func(ctx context.Context, jobID string) error

func (f processorFunc) Process(ctx context.Context, jobID string) (time.Time, error) {
	return time.Time{}, f(ctx, jobID)
}

// retryingProcessor asks for every job to be run again at the given time.
type retryingProcessor struct{ at time.Time }

func (p retryingProcessor) Process(context.Context, string) (time.Time, error) { return p.at, nil }

func testOptions(concurrency int) Options {
	return Options{Concurrency: concurrency, PollInterval: 5 * time.Millisecond, JobTimeout: 5 * time.Second, Lease: 30 * time.Second}
}

// start runs the Runner in the background and returns a stop function that cancels it and
// waits for Run to return.
func start(t *testing.T, q Queue, p Processor, opts Options) (stop func()) {
	t.Helper()
	r, err := NewRunner(q, p, opts, testutil.QuietLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("Run did not return after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRunner_ProcessesAndAcksEveryJob(t *testing.T) {
	q := &fakeQueue{pending: []string{"a", "b", "c", "d", "e"}}
	var processed atomic.Int32
	start(t, q, processorFunc(func(context.Context, string) error { processed.Add(1); return nil }), testOptions(2))

	waitFor(t, "all jobs acked", func() bool { return len(q.ackedJobs()) == 5 })
	if got := q.ackedJobs(); !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) || processed.Load() != 5 {
		t.Fatalf("acked %v after %d runs, want each job exactly once", got, processed.Load())
	}
}

func TestRunner_DoesNotAckJobsThatFailOrPanic(t *testing.T) {
	q := &fakeQueue{pending: []string{"ok-1", "fails", "panics", "ok-2"}}
	var finished atomic.Int32
	start(t, q, processorFunc(func(_ context.Context, id string) error {
		defer finished.Add(1)
		switch id {
		case "fails":
			return errors.New("db down")
		case "panics":
			panic("boom")
		}
		return nil
	}), testOptions(2))

	waitFor(t, "all four jobs to finish", func() bool { return finished.Load() == 4 })
	waitFor(t, "the healthy jobs to be acked", func() bool { return len(q.ackedJobs()) == 2 })
	if got := q.ackedJobs(); !slices.Equal(got, []string{"ok-1", "ok-2"}) {
		t.Fatalf("acked %v; failed and panicking jobs must stay un-acked so the queue redelivers them", got)
	}
}

func TestRunner_RequeuesJobsThatAskForARetry(t *testing.T) {
	q := &fakeQueue{pending: []string{"a", "b"}}
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	start(t, q, retryingProcessor{at: at}, testOptions(2))

	waitFor(t, "both jobs to be rescheduled", func() bool { return len(q.retriedJobs()) == 2 })
	for _, call := range q.retriedJobs() {
		if !call.at.Equal(at) {
			t.Errorf("job %s retried at %v, want %v", call.id, call.at, at)
		}
	}
	if len(q.ackedJobs()) != 0 {
		t.Fatalf("acked %v; a job that asked for a retry is not finished", q.ackedJobs())
	}
}

func TestRunner_ASchedulingFailureDoesNotAckTheJob(t *testing.T) {
	q := &fakeQueue{pending: []string{"a"}, retryErr: errors.New("redis: connection refused")}
	start(t, q, retryingProcessor{at: time.Now().Add(time.Hour)}, testOptions(1))

	waitFor(t, "the retry to be attempted", func() bool { return len(q.retriedJobs()) == 1 })
	time.Sleep(20 * time.Millisecond)
	if len(q.ackedJobs()) != 0 {
		t.Fatal("the job was acked although its retry could not be scheduled; it would be lost")
	}
}

func TestRunner_NeverRunsMoreJobsThanItsConcurrency(t *testing.T) {
	const concurrency, jobs = 3, 12
	q := &fakeQueue{}
	for i := 0; i < jobs; i++ {
		q.pending = append(q.pending, string(rune('a'+i)))
	}

	var running, peak atomic.Int32
	start(t, q, processorFunc(func(context.Context, string) error {
		n := running.Add(1)
		for { // record the high-water mark
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		running.Add(-1)
		return nil
	}), testOptions(concurrency))

	waitFor(t, "all jobs acked", func() bool { return len(q.ackedJobs()) == jobs })
	if peak.Load() > concurrency {
		t.Fatalf("%d jobs ran at once, limit is %d", peak.Load(), concurrency)
	}
	if peak.Load() < concurrency {
		t.Fatalf("peak concurrency was %d; the pool should have used all %d slots", peak.Load(), concurrency)
	}
	if q.maxLimit() > concurrency {
		t.Fatalf("claimed up to %d jobs at once with %d slots: jobs would be leased that cannot run", q.maxLimit(), concurrency)
	}
}

func TestRunner_ShutdownLetsRunningJobsFinish(t *testing.T) {
	q := &fakeQueue{pending: []string{"slow"}}
	release, started := make(chan struct{}), make(chan struct{})
	var sawCancelled atomic.Bool
	stop := start(t, q, processorFunc(func(ctx context.Context, _ string) error {
		close(started)
		<-release
		sawCancelled.Store(ctx.Err() != nil)
		return nil
	}), testOptions(1))

	<-started
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()

	select {
	case <-stopped:
		t.Fatal("Run returned while a job was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the job finished")
	}

	if sawCancelled.Load() {
		t.Error("the job's context was cancelled by shutdown; in-flight jobs must be allowed to finish")
	}
	if got := q.ackedJobs(); !slices.Equal(got, []string{"slow"}) {
		t.Fatalf("acked %v, want the in-flight job acked after it finished", got)
	}
}

func TestRunner_EnforcesTheJobTimeout(t *testing.T) {
	q := &fakeQueue{pending: []string{"stuck"}}
	opts := testOptions(1)
	opts.JobTimeout, opts.Lease = 50*time.Millisecond, time.Second

	var timedOut atomic.Bool
	start(t, q, processorFunc(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		timedOut.Store(true)
		return ctx.Err()
	}), opts)

	waitFor(t, "the job to hit its timeout", timedOut.Load)
	time.Sleep(20 * time.Millisecond)
	if len(q.ackedJobs()) != 0 {
		t.Fatal("a job that timed out must not be acked")
	}
}

func TestRunner_SurvivesQueueErrors(t *testing.T) {
	q := &fakeQueue{pending: []string{"a"}, claimErrs: 3}
	start(t, q, processorFunc(func(context.Context, string) error { return nil }), testOptions(1))

	waitFor(t, "the job to be processed once the queue recovers", func() bool { return len(q.ackedJobs()) == 1 })
}

func TestRunner_StopsPromptlyWhenIdle(t *testing.T) {
	q := &fakeQueue{}
	stop := start(t, q, processorFunc(func(context.Context, string) error { return nil }), testOptions(2))
	time.Sleep(20 * time.Millisecond)
	stop() // fails the test itself if Run does not return in time
}

func TestNewRunner_RejectsInconsistentOptions(t *testing.T) {
	good := testOptions(2)
	cases := map[string]func(*Options){
		"no concurrency":         func(o *Options) { o.Concurrency = 0 },
		"no poll interval":       func(o *Options) { o.PollInterval = 0 },
		"no job timeout":         func(o *Options) { o.JobTimeout = 0 },
		"lease equals timeout":   func(o *Options) { o.Lease = o.JobTimeout },
		"lease shorter than job": func(o *Options) { o.Lease = o.JobTimeout / 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			opts := good
			mutate(&opts)
			if _, err := NewRunner(&fakeQueue{}, processorFunc(nil), opts, testutil.QuietLogger()); err == nil {
				t.Fatal("NewRunner accepted inconsistent options")
			}
		})
	}
	if _, err := NewRunner(&fakeQueue{}, processorFunc(nil), good, testutil.QuietLogger()); err != nil {
		t.Fatalf("NewRunner rejected valid options: %v", err)
	}
}

func TestEvery_RunsImmediatelyThenRepeatsAndSurvivesErrors(t *testing.T) {
	var runs atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Every(ctx, testutil.QuietLogger(), 5*time.Millisecond, "test", func(context.Context) error {
			if runs.Add(1)%2 == 0 {
				return errors.New("every other run fails")
			}
			return nil
		})
		close(done)
	}()

	waitFor(t, "at least four runs", func() bool { return runs.Load() >= 4 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Every did not stop after cancellation")
	}
}
