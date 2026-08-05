package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	tenantdb "github.com/samirthapa/webhook-platform/internal/features/tenant/db"
	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/database"
)

// ErrNotFound is returned by repositories when a row does not exist.
var ErrNotFound = apperror.ErrNotFound

// Repository is the persistence contract the service depends on.
type Repository interface {
	CreateTenantWithKey(ctx context.Context, t Tenant, k APIKey, keyHash []byte) error
	CreateAPIKey(ctx context.Context, k APIKey, keyHash []byte) error
	GetTenant(ctx context.Context, id uuid.UUID) (Tenant, error)
	ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, tenantID, keyID uuid.UUID) error
	FindActiveKeyByHash(ctx context.Context, keyHash []byte) (APIKey, error)
	TouchAPIKey(ctx context.Context, keyID uuid.UUID) error
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	pool *pgxpool.Pool
	q    *tenantdb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool, q: tenantdb.New(pool)}
}

func (r *pgRepository) CreateTenantWithKey(ctx context.Context, t Tenant, k APIKey, keyHash []byte) error {
	return database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.InsertTenant(ctx, tenantdb.InsertTenantParams{
			ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		if err := q.InsertAPIKey(ctx, insertKeyParams(k, keyHash)); err != nil {
			return fmt.Errorf("insert api key: %w", err)
		}
		return nil
	})
}

func (r *pgRepository) CreateAPIKey(ctx context.Context, k APIKey, keyHash []byte) error {
	err := r.q.InsertAPIKey(ctx, insertKeyParams(k, keyHash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation: tenant missing
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return nil
}

func (r *pgRepository) GetTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	row, err := r.q.GetTenant(ctx, id)
	if err != nil {
		return Tenant{}, database.WrapErr(err, "get tenant")
	}
	return Tenant{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, nil
}

func (r *pgRepository) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]APIKey, error) {
	rows, err := r.q.ListAPIKeys(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	keys := make([]APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, APIKey(row))
	}
	return keys, nil
}

// RevokeAPIKey is idempotent: revoking an already revoked key succeeds.
func (r *pgRepository) RevokeAPIKey(ctx context.Context, tenantID, keyID uuid.UUID) error {
	n, err := r.q.RevokeAPIKey(ctx, tenantdb.RevokeAPIKeyParams{ID: keyID, TenantID: tenantID})
	return database.RequireRow(n, err, "revoke api key")
}

func (r *pgRepository) FindActiveKeyByHash(ctx context.Context, keyHash []byte) (APIKey, error) {
	row, err := r.q.FindActiveAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return APIKey{}, database.WrapErr(err, "find api key")
	}
	return APIKey(row), nil
}

func (r *pgRepository) TouchAPIKey(ctx context.Context, keyID uuid.UUID) error {
	if err := r.q.TouchAPIKey(ctx, keyID); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

func insertKeyParams(k APIKey, keyHash []byte) tenantdb.InsertAPIKeyParams {
	return tenantdb.InsertAPIKeyParams{
		ID:        k.ID,
		TenantID:  k.TenantID,
		Name:      k.Name,
		Prefix:    k.Prefix,
		KeyHash:   keyHash,
		CreatedAt: k.CreatedAt,
	}
}
