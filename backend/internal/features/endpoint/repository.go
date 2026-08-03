package endpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	endpointdb "github.com/samirthapa/webhook-platform/internal/features/endpoint/db"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
)

// ErrNotFound is returned by repositories when no row matches the tenant and ID.
// A row belonging to another tenant is deliberately indistinguishable from a missing one.
var ErrNotFound = errors.New("endpoint: not found")

// Repository is the persistence contract the service depends on. Every method is tenant-scoped.
type Repository interface {
	Create(ctx context.Context, e Endpoint) error
	Get(ctx context.Context, tenantID, id uuid.UUID) (Endpoint, error)
	// List returns up to p.QueryLimit() endpoints, newest first, after the p.After cursor.
	List(ctx context.Context, tenantID uuid.UUID, p pagination.Params) ([]Endpoint, error)
	// Update persists the mutable settings: URL, Description and EventTypes.
	Update(ctx context.Context, e Endpoint) error
	UpdateSecret(ctx context.Context, tenantID, id uuid.UUID, secret string, at time.Time) error
	Disable(ctx context.Context, tenantID, id uuid.UUID, reason string, at time.Time) error
	Enable(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	q *endpointdb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{q: endpointdb.New(pool)}
}

func (r *pgRepository) Create(ctx context.Context, e Endpoint) error {
	err := r.q.InsertEndpoint(ctx, endpointdb.InsertEndpointParams{
		ID:          e.ID,
		TenantID:    e.TenantID,
		URL:         e.URL,
		Description: e.Description,
		EventTypes:  nonNil(e.EventTypes),
		Secret:      e.Secret,
		Status:      string(e.Status),
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert endpoint: %w", err)
	}
	return nil
}

func (r *pgRepository) Get(ctx context.Context, tenantID, id uuid.UUID) (Endpoint, error) {
	row, err := r.q.GetEndpoint(ctx, endpointdb.GetEndpointParams{ID: id, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Endpoint{}, ErrNotFound
	}
	if err != nil {
		return Endpoint{}, fmt.Errorf("get endpoint: %w", err)
	}
	return toDomain(row), nil
}

func (r *pgRepository) List(ctx context.Context, tenantID uuid.UUID, p pagination.Params) ([]Endpoint, error) {
	rows, err := r.q.ListEndpoints(ctx, endpointdb.ListEndpointsParams{
		TenantID: tenantID,
		After:    p.After,
		RowLimit: p.QueryLimit(),
	})
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	out := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomain(row))
	}
	return out, nil
}

func (r *pgRepository) Update(ctx context.Context, e Endpoint) error {
	n, err := r.q.UpdateEndpoint(ctx, endpointdb.UpdateEndpointParams{
		URL:         e.URL,
		Description: e.Description,
		EventTypes:  nonNil(e.EventTypes),
		UpdatedAt:   e.UpdatedAt,
		ID:          e.ID,
		TenantID:    e.TenantID,
	})
	return requireRow(n, err, "update endpoint")
}

func (r *pgRepository) UpdateSecret(ctx context.Context, tenantID, id uuid.UUID, secret string, at time.Time) error {
	n, err := r.q.UpdateEndpointSecret(ctx, endpointdb.UpdateEndpointSecretParams{
		Secret: secret, At: at, ID: id, TenantID: tenantID,
	})
	return requireRow(n, err, "update endpoint secret")
}

func (r *pgRepository) Disable(ctx context.Context, tenantID, id uuid.UUID, reason string, at time.Time) error {
	n, err := r.q.DisableEndpoint(ctx, endpointdb.DisableEndpointParams{
		Reason: reason, At: at, ID: id, TenantID: tenantID,
	})
	return requireRow(n, err, "disable endpoint")
}

func (r *pgRepository) Enable(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	n, err := r.q.EnableEndpoint(ctx, endpointdb.EnableEndpointParams{At: at, ID: id, TenantID: tenantID})
	return requireRow(n, err, "enable endpoint")
}

func (r *pgRepository) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := r.q.DeleteEndpoint(ctx, endpointdb.DeleteEndpointParams{ID: id, TenantID: tenantID})
	return requireRow(n, err, "delete endpoint")
}

// requireRow turns "no rows affected" into ErrNotFound and wraps driver errors with context.
func requireRow(affected int64, err error, op string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func toDomain(row endpointdb.Endpoint) Endpoint {
	return Endpoint{
		ID:             row.ID,
		TenantID:       row.TenantID,
		URL:            row.URL,
		Description:    row.Description,
		EventTypes:     row.EventTypes,
		Secret:         row.Secret,
		Status:         Status(row.Status),
		DisabledReason: row.DisabledReason,
		DisabledAt:     row.DisabledAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// nonNil makes sure a nil slice is stored as an empty array, not NULL (the column is NOT NULL).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
