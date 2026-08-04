package event

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventdb "github.com/samirthapa/webhook-platform/internal/features/event/db"
	"github.com/samirthapa/webhook-platform/internal/shared/database"
	"github.com/samirthapa/webhook-platform/internal/shared/ids"
)

// ErrNotFound is returned by repositories when no row matches the tenant and key.
var ErrNotFound = errors.New("event: not found")

// CreateResult reports what Create did.
type CreateResult struct {
	// Created is false when the idempotency key was already used: nothing was written.
	Created bool
	// DeliveryIDs are the deliveries fanned out for a newly created event, one per matching endpoint.
	DeliveryIDs []uuid.UUID
}

// Repository is the persistence contract the service depends on.
type Repository interface {
	// Create stores the event and its fan-out deliveries atomically.
	Create(ctx context.Context, e Event) (CreateResult, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (Event, error)
	GetByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (Event, error)
}

const deliveryStatusPending = "pending"

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	pool *pgxpool.Pool
	q    *eventdb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool, q: eventdb.New(pool)}
}

// Create inserts the event, finds the endpoints that should receive it and inserts one delivery
// per endpoint, all in one transaction: an event never exists without its fan-out, and a
// delivery never exists without its event.
func (r *pgRepository) Create(ctx context.Context, e Event) (CreateResult, error) {
	var result CreateResult
	err := database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		inserted, err := q.InsertEvent(ctx, eventdb.InsertEventParams{
			ID:             e.ID,
			TenantID:       e.TenantID,
			Type:           e.Type,
			Payload:        e.Payload,
			IdempotencyKey: e.IdempotencyKey,
			CreatedAt:      e.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}
		if inserted == 0 {
			return nil // idempotent replay: an earlier request already stored this key
		}

		endpointIDs, err := q.ListMatchingEndpointIDs(ctx, eventdb.ListMatchingEndpointIDsParams{
			TenantID:  e.TenantID,
			EventType: e.Type,
		})
		if err != nil {
			return fmt.Errorf("find matching endpoints: %w", err)
		}

		rows := make([]eventdb.InsertDeliveriesParams, len(endpointIDs))
		deliveryIDs := make([]uuid.UUID, len(endpointIDs))
		for i, endpointID := range endpointIDs {
			deliveryIDs[i] = ids.New()
			rows[i] = eventdb.InsertDeliveriesParams{
				ID:         deliveryIDs[i],
				TenantID:   e.TenantID,
				EventID:    e.ID,
				EndpointID: endpointID,
				Status:     deliveryStatusPending,
				CreatedAt:  e.CreatedAt,
				UpdatedAt:  e.CreatedAt,
			}
		}
		if len(rows) > 0 {
			if _, err := q.InsertDeliveries(ctx, rows); err != nil {
				return fmt.Errorf("insert deliveries: %w", err)
			}
		}
		result = CreateResult{Created: true, DeliveryIDs: deliveryIDs}
		return nil
	})
	return result, err
}

func (r *pgRepository) Get(ctx context.Context, tenantID, id uuid.UUID) (Event, error) {
	row, err := r.q.GetEvent(ctx, eventdb.GetEventParams{ID: id, TenantID: tenantID})
	return toDomain(row, err, "get event")
}

func (r *pgRepository) GetByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (Event, error) {
	row, err := r.q.GetEventByIdempotencyKey(ctx, eventdb.GetEventByIdempotencyKeyParams{
		TenantID:       tenantID,
		IdempotencyKey: &key,
	})
	return toDomain(row, err, "get event by idempotency key")
}

func toDomain(row eventdb.Event, err error, op string) (Event, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("%s: %w", op, err)
	}
	return Event{
		ID:             row.ID,
		TenantID:       row.TenantID,
		Type:           row.Type,
		Payload:        row.Payload,
		IdempotencyKey: row.IdempotencyKey,
		CreatedAt:      row.CreatedAt,
	}, nil
}
