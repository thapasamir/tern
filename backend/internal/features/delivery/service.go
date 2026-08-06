package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
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
)

// JobQueue accepts jobs to run. It is implemented by shared/queue.
type JobQueue interface {
	Enqueue(ctx context.Context, runAt time.Time, jobIDs ...string) error
}

// Service is the API-facing side of deliveries: queueing them and reading their state.
type Service struct {
	repo Repository
	jobs JobQueue
	now  func() time.Time
	log  *slog.Logger
}

func NewService(repo Repository, jobs JobQueue) *Service {
	return &Service{repo: repo, jobs: jobs, now: time.Now, log: slog.Default()}
}

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

// ListByEvent returns the deliveries of one event, newest first.
func (s *Service) ListByEvent(ctx context.Context, tenantID, eventID uuid.UUID, p pagination.Params) (pagination.Result[Delivery], error) {
	rows, err := s.repo.ListByEvent(ctx, tenantID, eventID, p)
	if err != nil {
		return pagination.Result[Delivery]{}, apperror.Internal(err)
	}
	return pagination.NewResult(rows, p, func(d Delivery) uuid.UUID { return d.ID }), nil
}

func mapRepoError(err error) error { return apperror.FromStore(err, "delivery not found") }
