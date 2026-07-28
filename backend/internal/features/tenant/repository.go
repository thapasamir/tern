package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samirthapa/webhook-platform/internal/shared/database"
)

// ErrNotFound is returned by repositories when a row does not exist.
var ErrNotFound = errors.New("tenant: not found")

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

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository { return &pgRepository{pool: pool} }

const insertTenantSQL = `INSERT INTO tenants (id, name, created_at) VALUES ($1, $2, $3)`

const insertKeySQL = `
INSERT INTO api_keys (id, tenant_id, name, prefix, key_hash, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`

const keyColumns = `id, tenant_id, name, prefix, created_at, last_used_at, revoked_at`

func scanKey(row pgx.Row) (APIKey, error) {
	var k APIKey
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	return k, err
}

func (r *pgRepository) CreateTenantWithKey(ctx context.Context, t Tenant, k APIKey, keyHash []byte) error {
	return database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, insertTenantSQL, t.ID, t.Name, t.CreatedAt); err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		if _, err := tx.Exec(ctx, insertKeySQL, k.ID, k.TenantID, k.Name, k.Prefix, keyHash, k.CreatedAt); err != nil {
			return fmt.Errorf("insert api key: %w", err)
		}
		return nil
	})
}

func (r *pgRepository) CreateAPIKey(ctx context.Context, k APIKey, keyHash []byte) error {
	_, err := r.pool.Exec(ctx, insertKeySQL, k.ID, k.TenantID, k.Name, k.Prefix, keyHash, k.CreatedAt)
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
	var t Tenant
	err := r.pool.QueryRow(ctx, `SELECT id, name, created_at FROM tenants WHERE id = $1`, id).
		Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return t, nil
}

func (r *pgRepository) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeAPIKey is idempotent: revoking an already revoked key keeps its original revoked_at.
func (r *pgRepository) RevokeAPIKey(ctx context.Context, tenantID, keyID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1 AND tenant_id = $2`,
		keyID, tenantID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgRepository) FindActiveKeyByHash(ctx context.Context, keyHash []byte) (APIKey, error) {
	k, err := scanKey(r.pool.QueryRow(ctx,
		`SELECT `+keyColumns+` FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`, keyHash))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return APIKey{}, fmt.Errorf("find api key: %w", err)
	}
	return k, err
}

func (r *pgRepository) TouchAPIKey(ctx context.Context, keyID uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, keyID); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
