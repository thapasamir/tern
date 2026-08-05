package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	deliverydb "github.com/samirthapa/webhook-platform/internal/features/delivery/db"
	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/database"
	"github.com/samirthapa/webhook-platform/internal/shared/ids"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
)

// ErrNotFound is returned by repositories when no row matches.
var ErrNotFound = apperror.ErrNotFound

// Repository is the persistence contract the service and processor depend on.
type Repository interface {
	// GetWork loads what is needed to attempt a delivery. It is not tenant-scoped: it serves the
	// worker, whose job ids come from the platform's own queue.
	GetWork(ctx context.Context, id uuid.UUID) (Work, error)
	// RecordAttempt stores the attempt and completes the delivery in one transaction.
	// recorded is false when the delivery had already been completed by someone else.
	RecordAttempt(ctx context.Context, rec AttemptRecord) (recorded bool, err error)

	// The remaining methods back the API and are tenant-scoped.
	Get(ctx context.Context, tenantID, id uuid.UUID) (Delivery, error)
	ListAttempts(ctx context.Context, deliveryID uuid.UUID) ([]Attempt, error)
	ListByEvent(ctx context.Context, tenantID, eventID uuid.UUID, p pagination.Params) ([]Delivery, error)

	// ListUnenqueued and MarkEnqueued implement the outbox sweeper.
	ListUnenqueued(ctx context.Context, olderThan time.Time, limit int) ([]uuid.UUID, error)
	MarkEnqueued(ctx context.Context, deliveryIDs []uuid.UUID, at time.Time) error
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
		Status:         Status(row.DeliveryStatus),
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

		// Completing a still-pending delivery doubles as the lock that decides which of two
		// racing workers records its attempt: the loser matches no row.
		attemptNumber, err := q.CompleteDelivery(ctx, deliverydb.CompleteDeliveryParams{
			Status: string(rec.Outcome),
			At:     rec.FinishedAt,
			ID:     rec.DeliveryID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("complete delivery: %w", err)
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
		ID:           row.ID,
		TenantID:     row.TenantID,
		EventID:      row.EventID,
		EndpointID:   row.EndpointID,
		Status:       Status(row.Status),
		AttemptCount: int(row.AttemptCount),
		EnqueuedAt:   row.EnqueuedAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		CompletedAt:  row.CompletedAt,
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
