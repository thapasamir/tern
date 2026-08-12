package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	retentiondb "github.com/samirthapa/tern/internal/features/retention/db"
)

// Repository is the persistence contract the service depends on.
type Repository interface {
	// DeleteEventsBefore deletes up to limit events created before cutoff, along with their
	// deliveries and attempts, and returns how many events it deleted.
	DeleteEventsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	q *retentiondb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository { return &pgRepository{q: retentiondb.New(pool)} }

func (r *pgRepository) DeleteEventsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	n, err := r.q.DeleteEventsBefore(ctx, retentiondb.DeleteEventsBeforeParams{Cutoff: cutoff, RowLimit: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("delete events before %s: %w", cutoff.Format(time.RFC3339), err)
	}
	return n, nil
}
