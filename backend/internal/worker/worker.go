// Package worker runs background jobs: a pool that executes queued jobs, and helpers for
// periodic maintenance tasks. It knows nothing about deliveries; main wires it to them.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Queue is what the Runner needs from a job queue (see shared/queue).
type Queue interface {
	// Claim leases up to limit due jobs for the given duration.
	Claim(ctx context.Context, limit int, lease time.Duration) ([]string, error)
	// Ack marks a job as finished so its lease is not recovered.
	Ack(ctx context.Context, jobID string) error
}

// Processor executes one job. A nil error means the job is finished. An error means it could not
// be done right now: the Runner then does not acknowledge it, and the queue redelivers it once
// its lease expires.
type Processor interface {
	Process(ctx context.Context, jobID string) error
}

// Options configures a Runner.
type Options struct {
	// Concurrency is the maximum number of jobs running at once.
	Concurrency int
	// PollInterval is how long to wait before asking an empty (or failing) queue again.
	PollInterval time.Duration
	// JobTimeout bounds a single job.
	JobTimeout time.Duration
	// Lease is how long a claimed job stays invisible to other workers. It must exceed JobTimeout,
	// otherwise a job still running could be handed to a second worker.
	Lease time.Duration
}

func (o Options) validate() error {
	switch {
	case o.Concurrency < 1:
		return errors.New("worker: concurrency must be at least 1")
	case o.PollInterval <= 0:
		return errors.New("worker: poll interval must be positive")
	case o.JobTimeout <= 0:
		return errors.New("worker: job timeout must be positive")
	case o.Lease <= o.JobTimeout:
		return errors.New("worker: lease must be longer than the job timeout")
	}
	return nil
}

// Runner claims jobs from a Queue and runs them on a bounded pool of goroutines.
type Runner struct {
	queue Queue
	proc  Processor
	opts  Options
	log   *slog.Logger
}

// NewRunner returns a Runner, or an error if opts is inconsistent.
func NewRunner(q Queue, p Processor, opts Options, log *slog.Logger) (*Runner, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return &Runner{queue: q, proc: p, opts: opts, log: log}, nil
}

// Run processes jobs until ctx is cancelled, then waits for the jobs already running to finish
// before returning. In-flight jobs are not cancelled with ctx; each is bounded by JobTimeout.
func (r *Runner) Run(ctx context.Context) {
	slots := make(chan struct{}, r.opts.Concurrency) // a held slot = a job running or about to
	var inFlight sync.WaitGroup
	defer inFlight.Wait()

	for {
		// Wait for a free slot before claiming, so we never lease more jobs than we can run.
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}

		// We hold one slot already; claim up to that plus every slot that is still free.
		// Free slots can only grow while we are here (only this loop takes them), so the
		// slots acquired below can never block.
		free := cap(slots) - len(slots)
		jobs, err := r.queue.Claim(ctx, 1+free, r.opts.Lease)
		if err != nil || len(jobs) == 0 {
			<-slots
			if err != nil && ctx.Err() == nil {
				r.log.Error("claiming jobs failed", "error", err)
			}
			if !sleep(ctx, r.opts.PollInterval) {
				return
			}
			continue
		}

		for i := 1; i < len(jobs); i++ {
			slots <- struct{}{}
		}
		for _, id := range jobs {
			inFlight.Add(1)
			go func() {
				defer inFlight.Done()
				defer func() { <-slots }()
				r.run(ctx, id)
			}()
		}
	}
}

// run executes one job. It never lets a panic escape, since that would take down the process and
// every other job with it, and it never acknowledges a job that did not complete.
func (r *Runner) run(ctx context.Context, jobID string) {
	// Detached from ctx so that shutting down does not abort work that is already under way.
	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.JobTimeout)
	defer cancel()

	defer func() {
		if p := recover(); p != nil {
			r.log.Error("job panicked; it will be redelivered when its lease expires",
				"job", jobID, "panic", p, "stack", string(debug.Stack()))
		}
	}()

	if err := r.proc.Process(jobCtx, jobID); err != nil {
		r.log.Error("job failed; it will be redelivered when its lease expires", "job", jobID, "error", err)
		return
	}
	if err := r.queue.Ack(jobCtx, jobID); err != nil {
		r.log.Error("acknowledging job failed; it may run again", "job", jobID, "error", err)
	}
}

// Every runs fn immediately and then once per interval until ctx is cancelled. A failing run is
// logged and the loop carries on: maintenance tasks must not die because of one bad tick.
func Every(ctx context.Context, log *slog.Logger, interval time.Duration, name string, fn func(context.Context) error) {
	for {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			log.Error("periodic task failed", "task", name, "error", err)
		}
		if !sleep(ctx, interval) {
			return
		}
	}
}

// sleep waits for d and reports whether it completed (false if ctx ended first).
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
