package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	deliverydb "github.com/samirthapa/tern/internal/features/delivery/db"
	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/database"
	"github.com/samirthapa/tern/internal/shared/ids"
	"github.com/samirthapa/tern/internal/shared/pagination"
)

// ErrNotFound is returned by repositories when no row matches.
var ErrNotFound = apperror.ErrNotFound

// Repository is the persistence contract the service and processor depend on.
type Repository interface {
	// GetWork loads what is needed to attempt a delivery. It is not tenant-scoped: it serves the
	// worker, whose job ids come from the platform's own queue.
	GetWork(ctx context.Context, id uuid.UUID) (Work, error)
	// RecordAttempt stores the attempt and updates the delivery in one transaction: it completes
	// the delivery, or, when rec asks for a retry, keeps it pending with its next due time.
	// recorded is false when the delivery had already been completed by someone else.
	RecordAttempt(ctx context.Context, rec AttemptRecord) (recorded bool, err error)
	// FailPendingForEndpoint fails every pending delivery of an endpoint that was just disabled,
	// recording reason as the error of a final attempt on each. It returns how many it failed.
	FailPendingForEndpoint(ctx context.Context, endpointID uuid.UUID, reason string, at time.Time) (int64, error)

	// The remaining methods back the API and are tenant-scoped.
	Get(ctx context.Context, tenantID, id uuid.UUID) (Delivery, error)
	ListAttempts(ctx context.Context, deliveryID uuid.UUID) ([]Attempt, error)
	ListByEvent(ctx context.Context, tenantID, eventID uuid.UUID, p pagination.Params) ([]Delivery, error)

	// ListUnenqueued and MarkEnqueued implement the outbox sweeper.
	ListUnenqueued(ctx context.Context, olderThan time.Time, limit int) ([]uuid.UUID, error)
	MarkEnqueued(ctx context.Context, deliveryIDs []uuid.UUID, at time.Time) error
	// ListOverdue returns pending deliveries that are due before dueBefore and were not touched
	// since, for the reconciler that re-queues jobs lost with the queue's data.
	ListOverdue(ctx context.Context, dueBefore time.Time, limit int) ([]uuid.UUID, error)

	// GetReplaySource loads a delivery and the state of its endpoint, for deciding on a replay.
	GetReplaySource(ctx context.Context, tenantID, deliveryID uuid.UUID) (ReplaySource, error)
	// EndpointActive reports whether the tenant's endpoint is active; ErrNotFound if it does not exist.
	EndpointActive(ctx context.Context, tenantID, endpointID uuid.UUID) (bool, error)
	// CreateReplay creates a new pending delivery that repeats src.
	CreateReplay(ctx context.Context, src ReplaySource, at time.Time) (Delivery, error)
	// ReplayFailed creates a replay of up to limit failed deliveries of an endpoint created in
	// [since, until) that have no replay yet, atomically.
	ReplayFailed(ctx context.Context, tenantID, endpointID uuid.UUID, since, until time.Time, limit int, at time.Time) (ReplayBatch, error)
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	pool *pgxpool.Pool
	q    *deliverydb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool, q: deliverydb.New(pool)}
}

func (r *pgRepository) GetWork(ctx context.Context, id uuid.UUID) (Work, error) {
	row, err := r.q.GetDeliveryWork(ctx, id)
	if err != nil {
		return Work{}, database.WrapErr(err, "get delivery work")
	}
	return Work{
		DeliveryID:     row.DeliveryID,
		TenantID:       row.TenantID,
		EndpointID:     row.EndpointID,
		Status:         Status(row.DeliveryStatus),
		AttemptCount:   int(row.AttemptCount),
		NextAttemptAt:  row.NextAttemptAt,
		EventID:        row.EventID,
		EventType:      row.EventType,
		Payload:        row.EventPayload,
		EventCreatedAt: row.EventCreatedAt,
		EndpointURL:    row.EndpointURL,
		EndpointSecret: row.EndpointSecret,
		EndpointActive: row.EndpointActive,
	}, nil
}

func (r *pgRepository) RecordAttempt(ctx context.Context, rec AttemptRecord) (bool, error) {
	recorded := false
	err := database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		// Updating a still-pending delivery doubles as the lock that decides which of two
		// racing workers records its attempt: the loser matches no row.
		attemptNumber, err := r.updateDelivery(ctx, q, rec)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("update delivery: %w", err)
		}

		if err := q.InsertAttempt(ctx, deliverydb.InsertAttemptParams{
			ID:            ids.New(),
			DeliveryID:    rec.DeliveryID,
			AttemptNumber: attemptNumber,
			StatusCode:    toInt32Ptr(rec.StatusCode),
			Error:         rec.Error,
			ResponseBody:  rec.ResponseBody,
			DurationMs:    int32(rec.Duration.Milliseconds()),
			StartedAt:     rec.StartedAt,
		}); err != nil {
			return fmt.Errorf("insert attempt: %w", err)
		}
		recorded = true
		return nil
	})
	return recorded, err
}

// updateDelivery applies an attempt's outcome to its delivery and returns the attempt's number.
func (r *pgRepository) updateDelivery(ctx context.Context, q *deliverydb.Queries, rec AttemptRecord) (int32, error) {
	if rec.Outcome != StatusPending {
		return q.CompleteDelivery(ctx, deliverydb.CompleteDeliveryParams{
			Status: string(rec.Outcome),
			At:     rec.FinishedAt,
			ID:     rec.DeliveryID,
		})
	}
	if rec.RetryAt == nil {
		return 0, errors.New("attempt record asks for a retry but has no RetryAt")
	}
	return q.ScheduleRetry(ctx, deliverydb.ScheduleRetryParams{
		NextAttemptAt: *rec.RetryAt,
		At:            rec.FinishedAt,
		ID:            rec.DeliveryID,
	})
}

func (r *pgRepository) FailPendingForEndpoint(ctx context.Context, endpointID uuid.UUID, reason string, at time.Time) (int64, error) {
	n, err := r.q.FailPendingForEndpoint(ctx, deliverydb.FailPendingForEndpointParams{
		EndpointID: endpointID,
		Reason:     reason,
		At:         at,
	})
	if err != nil {
		return 0, database.WrapErr(err, "fail pending deliveries of endpoint")
	}
	return n, nil
}

func (r *pgRepository) ListOverdue(ctx context.Context, dueBefore time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := r.q.ListOverdueDeliveries(ctx, deliverydb.ListOverdueDeliveriesParams{
		DueBefore: dueBefore,
		RowLimit:  int32(limit),
	})
	if err != nil {
		return nil, database.WrapErr(err, "list overdue deliveries")
	}
	return rows, nil
}

func (r *pgRepository) GetReplaySource(ctx context.Context, tenantID, deliveryID uuid.UUID) (ReplaySource, error) {
	row, err := r.q.GetReplaySource(ctx, deliverydb.GetReplaySourceParams{ID: deliveryID, TenantID: tenantID})
	if err != nil {
		return ReplaySource{}, database.WrapErr(err, "get replay source")
	}
	return ReplaySource{
		DeliveryID:     row.ID,
		TenantID:       row.TenantID,
		EventID:        row.EventID,
		EndpointID:     row.EndpointID,
		Status:         Status(row.Status),
		EndpointActive: row.EndpointActive,
	}, nil
}

func (r *pgRepository) EndpointActive(ctx context.Context, tenantID, endpointID uuid.UUID) (bool, error) {
	active, err := r.q.GetEndpointActive(ctx, deliverydb.GetEndpointActiveParams{ID: endpointID, TenantID: tenantID})
	if err != nil {
		return false, database.WrapErr(err, "get endpoint state")
	}
	return active, nil
}

func (r *pgRepository) CreateReplay(ctx context.Context, src ReplaySource, at time.Time) (Delivery, error) {
	d := Delivery{
		ID:           ids.New(),
		TenantID:     src.TenantID,
		EventID:      src.EventID,
		EndpointID:   src.EndpointID,
		Status:       StatusPending,
		ReplayedFrom: &src.DeliveryID,
		CreatedAt:    at,
		UpdatedAt:    at,
	}
	if _, err := r.q.InsertReplays(ctx, []deliverydb.InsertReplaysParams{replayRow(d)}); err != nil {
		return Delivery{}, fmt.Errorf("insert replay: %w", err)
	}
	return d, nil
}

func (r *pgRepository) ReplayFailed(ctx context.Context, tenantID, endpointID uuid.UUID, since, until time.Time, limit int, at time.Time) (ReplayBatch, error) {
	var batch ReplayBatch
	err := database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		// Fetch one more than the limit to learn whether there is more, without a COUNT.
		claimed, err := q.ClaimFailedForReplay(ctx, deliverydb.ClaimFailedForReplayParams{
			TenantID: tenantID, EndpointID: endpointID, Since: since, Until: until, RowLimit: int32(limit + 1),
		})
		if err != nil {
			return fmt.Errorf("claim failed deliveries: %w", err)
		}
		if len(claimed) > limit {
			batch.HasMore = true
			claimed = claimed[:limit]
		}

		rows := make([]deliverydb.InsertReplaysParams, len(claimed))
		batch.IDs = make([]uuid.UUID, len(claimed))
		for i, original := range claimed {
			replay := Delivery{
				ID: ids.New(), TenantID: tenantID, EventID: original.EventID, EndpointID: endpointID,
				Status: StatusPending, ReplayedFrom: &claimed[i].ID, CreatedAt: at, UpdatedAt: at,
			}
			rows[i], batch.IDs[i] = replayRow(replay), replay.ID
		}
		if len(rows) > 0 {
			if _, err := q.InsertReplays(ctx, rows); err != nil {
				return fmt.Errorf("insert replays: %w", err)
			}
		}
		return nil
	})
	return batch, err
}

func replayRow(d Delivery) deliverydb.InsertReplaysParams {
	return deliverydb.InsertReplaysParams{
		ID: d.ID, TenantID: d.TenantID, EventID: d.EventID, EndpointID: d.EndpointID,
		Status: string(d.Status), ReplayedFrom: d.ReplayedFrom, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func (r *pgRepository) Get(ctx context.Context, tenantID, id uuid.UUID) (Delivery, error) {
	row, err := r.q.GetDelivery(ctx, deliverydb.GetDeliveryParams{ID: id, TenantID: tenantID})
	if err != nil {
		return Delivery{}, database.WrapErr(err, "get delivery")
	}
	return toDelivery(row), nil
}

func (r *pgRepository) ListAttempts(ctx context.Context, deliveryID uuid.UUID) ([]Attempt, error) {
	rows, err := r.q.ListAttempts(ctx, deliveryID)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	out := make([]Attempt, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAttempt(row))
	}
	return out, nil
}

func (r *pgRepository) ListByEvent(ctx context.Context, tenantID, eventID uuid.UUID, p pagination.Params) ([]Delivery, error) {
	rows, err := r.q.ListDeliveriesByEvent(ctx, deliverydb.ListDeliveriesByEventParams{
		TenantID: tenantID,
		EventID:  eventID,
		After:    p.After,
		RowLimit: p.QueryLimit(),
	})
	if err != nil {
		return nil, fmt.Errorf("list deliveries by event: %w", err)
	}
	out := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDelivery(row))
	}
	return out, nil
}

func (r *pgRepository) ListUnenqueued(ctx context.Context, olderThan time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := r.q.ListUnenqueuedDeliveries(ctx, deliverydb.ListUnenqueuedDeliveriesParams{
		OlderThan: olderThan,
		RowLimit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list unenqueued deliveries: %w", err)
	}
	return rows, nil
}

func (r *pgRepository) MarkEnqueued(ctx context.Context, deliveryIDs []uuid.UUID, at time.Time) error {
	if err := r.q.MarkDeliveriesEnqueued(ctx, deliverydb.MarkDeliveriesEnqueuedParams{Ids: deliveryIDs, At: at}); err != nil {
		return fmt.Errorf("mark deliveries enqueued: %w", err)
	}
	return nil
}

func toDelivery(row deliverydb.Delivery) Delivery {
	return Delivery{
		ID:            row.ID,
		TenantID:      row.TenantID,
		EventID:       row.EventID,
		EndpointID:    row.EndpointID,
		Status:        Status(row.Status),
		AttemptCount:  int(row.AttemptCount),
		NextAttemptAt: row.NextAttemptAt,
		ReplayedFrom:  row.ReplayedFrom,
		EnqueuedAt:    row.EnqueuedAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		CompletedAt:   row.CompletedAt,
	}
}

func toAttempt(row deliverydb.DeliveryAttempt) Attempt {
	a := Attempt{
		ID:           row.ID,
		DeliveryID:   row.DeliveryID,
		Number:       int(row.AttemptNumber),
		Error:        row.Error,
		ResponseBody: row.ResponseBody,
		Duration:     time.Duration(row.DurationMs) * time.Millisecond,
		StartedAt:    row.StartedAt,
	}
	if row.StatusCode != nil {
		code := int(*row.StatusCode)
		a.StatusCode = &code
	}
	return a
}

func toInt32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}
