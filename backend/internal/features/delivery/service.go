package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/pagination"
)

const (
	// orphanGrace is how long a delivery may stay un-queued before the sweeper steps in. It must
	// comfortably exceed the time the API normally needs between COMMIT and the Redis write.
	orphanGrace = 30 * time.Second
	orphanBatch = 100

	// overdueGrace is how late a delivery must be before the reconciler assumes its job was lost.
	// Generous on purpose: a busy queue legitimately runs jobs a little late.
	overdueGrace = 5 * time.Minute
	overdueBatch = 500

	// maxBulkReplay is how many failed deliveries one bulk replay creates. A bigger backlog is
	// replayed by calling again: replayed deliveries are skipped, so it simply continues.
	maxBulkReplay  = 1000
	enqueueTimeout = 5 * time.Second
)

// JobQueue accepts jobs to run. It is implemented by shared/queue.
type JobQueue interface {
	Enqueue(ctx context.Context, runAt time.Time, jobIDs ...string) error
}

// Service is the API-facing side of deliveries: queueing them and reading their state.
type Service struct {
	repo      Repository
	jobs      JobQueue
	now       func() time.Time
	log       *slog.Logger
	bulkLimit int // deliveries per bulk replay
}

func NewService(repo Repository, jobs JobQueue) *Service {
	return &Service{repo: repo, jobs: jobs, now: time.Now, log: slog.Default(), bulkLimit: maxBulkReplay}
}

var errEndpointDisabledForReplay = apperror.Conflict("endpoint is disabled; enable it before replaying")

// Enqueue puts deliveries on the queue and records that it did. It is idempotent, so it is safe
// to call again for the same deliveries after a partial failure.
func (s *Service) Enqueue(ctx context.Context, deliveryIDs []uuid.UUID) error {
	if len(deliveryIDs) == 0 {
		return nil
	}
	now := s.now()
	jobIDs := make([]string, len(deliveryIDs))
	for i, id := range deliveryIDs {
		jobIDs[i] = id.String()
	}
	if err := s.jobs.Enqueue(ctx, now, jobIDs...); err != nil {
		return fmt.Errorf("enqueue deliveries: %w", err)
	}
	if err := s.repo.MarkEnqueued(ctx, deliveryIDs, now); err != nil {
		// The jobs are queued; only the bookkeeping failed. The sweeper will enqueue them
		// again (a no-op, since Enqueue is idempotent) and then record it.
		return err
	}
	return nil
}

// EnqueueOrphans is the outbox sweeper. A delivery row is committed before its job is pushed to
// Redis, so a crash or a Redis outage in between leaves a pending delivery that nothing will
// ever run. This finds those and queues them. The worker runs it periodically.
func (s *Service) EnqueueOrphans(ctx context.Context) error {
	deliveryIDs, err := s.repo.ListUnenqueued(ctx, s.now().Add(-orphanGrace), orphanBatch)
	if err != nil {
		return err
	}
	if len(deliveryIDs) == 0 {
		return nil
	}
	s.log.Warn("queueing deliveries that never reached the queue", "count", len(deliveryIDs))
	return s.Enqueue(ctx, deliveryIDs)
}

// ReconcileOverdue is the safety net behind the queue. Deliveries are scheduled in Redis, but
// Postgres holds the truth about when each is due, so if Redis loses its data (a restart without
// persistence, an eviction, a flush) the jobs vanish while the deliveries stay pending forever.
// This finds the deliveries that are long overdue and queues them again. Enqueueing is
// idempotent, so deliveries that are in fact still queued are unaffected.
func (s *Service) ReconcileOverdue(ctx context.Context) error {
	deliveryIDs, err := s.repo.ListOverdue(ctx, s.now().Add(-overdueGrace), overdueBatch)
	if err != nil {
		return err
	}
	if len(deliveryIDs) == 0 {
		return nil
	}
	s.log.Warn("re-queueing deliveries that are long overdue", "count", len(deliveryIDs))
	return s.Enqueue(ctx, deliveryIDs)
}

// Replay repeats a finished delivery: it creates a new delivery of the same event to the same
// endpoint, linked to the original, which keeps its own log untouched. A delivery that is still
// pending cannot be replayed (it will be attempted anyway), and neither can one whose endpoint is
// disabled, since the replay would only fail again.
func (s *Service) Replay(ctx context.Context, tenantID, deliveryID uuid.UUID) (Delivery, error) {
	src, err := s.repo.GetReplaySource(ctx, tenantID, deliveryID)
	if err != nil {
		return Delivery{}, mapRepoError(err)
	}
	switch {
	case src.Status == StatusPending:
		return Delivery{}, apperror.Conflict("delivery is still pending; wait for it to finish before replaying it")
	case !src.EndpointActive:
		return Delivery{}, errEndpointDisabledForReplay
	}

	replay, err := s.repo.CreateReplay(ctx, src, s.now().UTC())
	if err != nil {
		return Delivery{}, apperror.Internal(err)
	}
	s.enqueueAfterCommit(ctx, replay.ID)
	return replay, nil
}

// ReplayResult is the outcome of ReplayFailed.
type ReplayResult struct {
	Replayed int
	// HasMore is true when more deliveries matched than one call replays; call again to continue.
	HasMore bool
}

// ReplayFailed replays the failed deliveries of an endpoint that were created in [since, until)
// and have not been replayed yet, up to a limit per call. A zero until means now. Calling it again
// with the same range is safe: deliveries that already have a replay are skipped.
func (s *Service) ReplayFailed(ctx context.Context, tenantID, endpointID uuid.UUID, since, until time.Time) (ReplayResult, error) {
	now := s.now().UTC()
	if until.IsZero() {
		until = now
	}
	switch {
	case since.IsZero():
		return ReplayResult{}, apperror.Invalid("since is required")
	case !since.Before(until):
		return ReplayResult{}, apperror.Invalid("since must be before until")
	}

	active, err := s.repo.EndpointActive(ctx, tenantID, endpointID)
	if errors.Is(err, ErrNotFound) {
		return ReplayResult{}, apperror.NotFound("endpoint not found")
	}
	if err != nil {
		return ReplayResult{}, apperror.Internal(err)
	}
	if !active {
		return ReplayResult{}, errEndpointDisabledForReplay
	}

	batch, err := s.repo.ReplayFailed(ctx, tenantID, endpointID, since.UTC(), until.UTC(), s.bulkLimit, now)
	if err != nil {
		return ReplayResult{}, apperror.Internal(err)
	}
	s.enqueueAfterCommit(ctx, batch.IDs...)
	return ReplayResult{Replayed: len(batch.IDs), HasMore: batch.HasMore}, nil
}

// enqueueAfterCommit queues freshly created deliveries. The replays are already stored, so a
// queue failure is logged, not returned: the outbox sweeper queues them shortly, and failing the
// request would invite a second replay of the same deliveries.
func (s *Service) enqueueAfterCommit(ctx context.Context, deliveryIDs ...uuid.UUID) {
	if len(deliveryIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), enqueueTimeout)
	defer cancel()
	if err := s.Enqueue(ctx, deliveryIDs); err != nil {
		s.log.Error("enqueue after commit failed; the outbox sweeper will retry",
			"deliveries", len(deliveryIDs), "error", err)
	}
}

// GetWithAttempts returns a delivery and the record of every attempt made for it.
func (s *Service) GetWithAttempts(ctx context.Context, tenantID, id uuid.UUID) (Delivery, []Attempt, error) {
	d, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return Delivery{}, nil, mapRepoError(err)
	}
	attempts, err := s.repo.ListAttempts(ctx, d.ID)
	if err != nil {
		return Delivery{}, nil, apperror.Internal(err)
	}
	return d, attempts, nil
}

// List returns the delivery log: the tenant's deliveries matching the filter, newest first.
func (s *Service) List(ctx context.Context, tenantID uuid.UUID, f ListFilter, p pagination.Params) (pagination.Result[Delivery], error) {
	if f.Status != nil {
		if _, ok := ParseStatus(string(*f.Status)); !ok {
			return pagination.Result[Delivery]{}, apperror.Invalid("status must be one of: pending, succeeded, failed")
		}
	}
	if f.Since != nil && f.Until != nil && !f.Since.Before(*f.Until) {
		return pagination.Result[Delivery]{}, apperror.Invalid("since must be before until")
	}

	rows, err := s.repo.List(ctx, tenantID, f, p)
	if err != nil {
		return pagination.Result[Delivery]{}, apperror.Internal(err)
	}
	return pagination.NewResult(rows, p, func(d Delivery) uuid.UUID { return d.ID }), nil
}

func mapRepoError(err error) error { return apperror.FromStore(err, "delivery not found") }
