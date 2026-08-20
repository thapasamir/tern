package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	userdb "github.com/samirthapa/tern/internal/features/user/db"
	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/database"
)

var (
	// ErrNotFound is returned when no row matches.
	ErrNotFound = apperror.ErrNotFound
	// ErrEmailTaken is returned when an account with that email already exists.
	ErrEmailTaken = errors.New("user: email already registered")
)

// uniqueViolation is Postgres' error code for a duplicate in a unique index.
const uniqueViolation = "23505"

// Repository is the persistence contract the service depends on. Tokens are passed as hashes:
// the plaintext never reaches the database layer.
type Repository interface {
	// CreateAccount stores a new tenant, its first user and their session in one transaction.
	// It returns ErrEmailTaken if the email is already registered, in which case nothing is stored.
	CreateAccount(ctx context.Context, t Tenant, u User, s Session, tokenHash []byte) error
	CreateSession(ctx context.Context, s Session, tokenHash []byte) error
	FindUserByEmail(ctx context.Context, email string) (User, error)
	GetAccount(ctx context.Context, userID uuid.UUID) (Account, error)
	// FindSession returns the owner of the session with that token hash, if it has not expired.
	FindSession(ctx context.Context, tokenHash []byte, now time.Time) (Principal, error)
	DeleteSession(ctx context.Context, sessionID uuid.UUID) error
	// DeleteExpiredSessions removes the user's sessions that ended before now.
	DeleteExpiredSessions(ctx context.Context, userID uuid.UUID, now time.Time) error
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	pool *pgxpool.Pool
	q    *userdb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool, q: userdb.New(pool)}
}

func (r *pgRepository) CreateAccount(ctx context.Context, t Tenant, u User, s Session, tokenHash []byte) error {
	err := database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.InsertTenant(ctx, userdb.InsertTenantParams{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt}); err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		if err := q.InsertUser(ctx, userdb.InsertUserParams{
			ID: u.ID, TenantID: u.TenantID, Email: u.Email, PasswordHash: u.PasswordHash, CreatedAt: u.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert user: %w", err)
		}
		return r.insertSession(ctx, q, s, tokenHash)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "users_email_idx" {
		return ErrEmailTaken
	}
	return err
}

func (r *pgRepository) CreateSession(ctx context.Context, s Session, tokenHash []byte) error {
	return r.insertSession(ctx, r.q, s, tokenHash)
}

func (r *pgRepository) insertSession(ctx context.Context, q *userdb.Queries, s Session, tokenHash []byte) error {
	if err := q.InsertSession(ctx, userdb.InsertSessionParams{
		ID: s.ID, UserID: s.UserID, TokenHash: tokenHash, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *pgRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return User{}, database.WrapErr(err, "get user by email")
	}
	return User{ID: row.ID, TenantID: row.TenantID, Email: row.Email, PasswordHash: row.PasswordHash, CreatedAt: row.CreatedAt}, nil
}

func (r *pgRepository) GetAccount(ctx context.Context, userID uuid.UUID) (Account, error) {
	row, err := r.q.GetAccount(ctx, userID)
	if err != nil {
		return Account{}, database.WrapErr(err, "get account")
	}
	return Account{
		User:   User{ID: row.UserID, TenantID: row.TenantID, Email: row.Email, CreatedAt: row.UserCreatedAt},
		Tenant: Tenant{ID: row.TenantID, Name: row.TenantName, CreatedAt: row.TenantCreatedAt},
	}, nil
}

func (r *pgRepository) FindSession(ctx context.Context, tokenHash []byte, now time.Time) (Principal, error) {
	row, err := r.q.FindSession(ctx, userdb.FindSessionParams{TokenHash: tokenHash, Now: now})
	if err != nil {
		return Principal{}, database.WrapErr(err, "find session")
	}
	return Principal{UserID: row.UserID, TenantID: row.TenantID, SessionID: row.SessionID}, nil
}

func (r *pgRepository) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	n, err := r.q.DeleteSession(ctx, sessionID)
	return database.RequireRow(n, err, "delete session")
}

func (r *pgRepository) DeleteExpiredSessions(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := r.q.DeleteExpiredSessions(ctx, userdb.DeleteExpiredSessionsParams{UserID: userID, Now: now}); err != nil {
		return database.WrapErr(err, "delete expired sessions")
	}
	return nil
}
