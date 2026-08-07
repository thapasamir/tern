package endpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	endpointdb "github.com/samirthapa/tern/internal/features/endpoint/db"
	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/database"
	"github.com/samirthapa/tern/internal/shared/pagination"
)

// ErrNotFound is returned by repositories when no row matches the tenant and ID.
// A row belonging to another tenant is deliberately indistinguishable from a missing one.
var ErrNotFound = apperror.ErrNotFound

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

	// IncrementFailures adds one to the endpoint's consecutive-failure count and returns the new count.
	IncrementFailures(ctx context.Context, tenantID, id uuid.UUID) (int, error)
	// ResetFailures sets the count back to zero. It is not an error if it already was zero.
	ResetFailures(ctx context.Context, tenantID, id uuid.UUID) error
	// DisableIfActive disables the endpoint only if it is currently active, and reports whether
	// it did. An endpoint that is already disabled (or gone) yields false, so of several callers
	// racing to disable the same endpoint exactly one gets true.
	DisableIfActive(ctx context.Context, tenantID, id uuid.UUID, reason string, at time.Time) (bool, error)
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
	if err != nil {
		return Endpoint{}, database.WrapErr(err, "get endpoint")
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
	return database.RequireRow(n, err, "update endpoint")
}

func (r *pgRepository) UpdateSecret(ctx context.Context, tenantID, id uuid.UUID, secret string, at time.Time) error {
	n, err := r.q.UpdateEndpointSecret(ctx, endpointdb.UpdateEndpointSecretParams{
		Secret: secret, At: at, ID: id, TenantID: tenantID,
	})
	return database.RequireRow(n, err, "update endpoint secret")
}

func (r *pgRepository) Disable(ctx context.Context, tenantID, id uuid.UUID, reason string, at time.Time) error {
	n, err := r.q.DisableEndpoint(ctx, endpointdb.DisableEndpointParams{
		Reason: reason, At: at, ID: id, TenantID: tenantID,
	})
	return database.RequireRow(n, err, "disable endpoint")
}

func (r *pgRepository) Enable(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	n, err := r.q.EnableEndpoint(ctx, endpointdb.EnableEndpointParams{At: at, ID: id, TenantID: tenantID})
	return database.RequireRow(n, err, "enable endpoint")
}

func (r *pgRepository) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	n, err := r.q.DeleteEndpoint(ctx, endpointdb.DeleteEndpointParams{ID: id, TenantID: tenantID})
	return database.RequireRow(n, err, "delete endpoint")
}

func (r *pgRepository) IncrementFailures(ctx context.Context, tenantID, id uuid.UUID) (int, error) {
	n, err := r.q.IncrementEndpointFailures(ctx, endpointdb.IncrementEndpointFailuresParams{ID: id, TenantID: tenantID})
	if err != nil {
		return 0, database.WrapErr(err, "increment endpoint failures")
	}
	return int(n), nil
}

func (r *pgRepository) ResetFailures(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := r.q.ResetEndpointFailures(ctx, endpointdb.ResetEndpointFailuresParams{ID: id, TenantID: tenantID}); err != nil {
		return database.WrapErr(err, "reset endpoint failures")
	}
	return nil
}

func (r *pgRepository) DisableIfActive(ctx context.Context, tenantID, id uuid.UUID, reason string, at time.Time) (bool, error) {
	n, err := r.q.DisableActiveEndpoint(ctx, endpointdb.DisableActiveEndpointParams{
		Reason: reason, At: at, ID: id, TenantID: tenantID,
	})
	if err != nil {
		return false, database.WrapErr(err, "disable endpoint if active")
	}
	return n == 1, nil
}

func toDomain(row endpointdb.Endpoint) Endpoint {
	return Endpoint{
		ID:                  row.ID,
		TenantID:            row.TenantID,
		URL:                 row.URL,
		Description:         row.Description,
		EventTypes:          row.EventTypes,
		Secret:              row.Secret,
		Status:              Status(row.Status),
		DisabledReason:      row.DisabledReason,
		DisabledAt:          row.DisabledAt,
		ConsecutiveFailures: int(row.ConsecutiveFailures),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

// nonNil makes sure a nil slice is stored as an empty array, not NULL (the column is NOT NULL).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
