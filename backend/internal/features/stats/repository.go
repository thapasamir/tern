package stats

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	statsdb "github.com/samirthapa/tern/internal/features/stats/db"
)

// Repository is the persistence contract the service depends on. Every method is tenant-scoped.
type Repository interface {
	CountByStatus(ctx context.Context, q Query) (Counts, error)
	ResponseTimes(ctx context.Context, q Query) (ResponseTimes, error)
	// Series returns the buckets that contain deliveries, oldest first. Empty buckets are left out.
	Series(ctx context.Context, q Query) ([]Bucket, error)
}

// pgRepository implements Repository on top of the sqlc-generated queries.
// SQL lives in queries.sql; run `make sqlc` after changing it.
type pgRepository struct {
	q *statsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) Repository { return &pgRepository{q: statsdb.New(pool)} }

func (r *pgRepository) CountByStatus(ctx context.Context, q Query) (Counts, error) {
	rows, err := r.q.CountDeliveriesByStatus(ctx, statsdb.CountDeliveriesByStatusParams{
		TenantID: q.TenantID, Since: q.Since, Until: q.Until, EndpointID: q.EndpointID,
	})
	if err != nil {
		return Counts{}, fmt.Errorf("count deliveries by status: %w", err)
	}
	var c Counts
	for _, row := range rows {
		c.add(row.Status, row.Total)
	}
	return c, nil
}

func (r *pgRepository) ResponseTimes(ctx context.Context, q Query) (ResponseTimes, error) {
	row, err := r.q.AttemptResponseTimes(ctx, statsdb.AttemptResponseTimesParams{
		TenantID: q.TenantID, Since: q.Since, Until: q.Until, EndpointID: q.EndpointID,
	})
	if err != nil {
		return ResponseTimes{}, fmt.Errorf("attempt response times: %w", err)
	}
	return ResponseTimes{Attempts: row.Attempts, AvgMs: row.AvgMs, P50Ms: row.P50Ms, P95Ms: row.P95Ms}, nil
}

func (r *pgRepository) Series(ctx context.Context, q Query) ([]Bucket, error) {
	rows, err := r.q.DeliverySeries(ctx, statsdb.DeliverySeriesParams{
		Unit: string(q.Interval), TenantID: q.TenantID, Since: q.Since, Until: q.Until, EndpointID: q.EndpointID,
	})
	if err != nil {
		return nil, fmt.Errorf("delivery series: %w", err)
	}

	// The query returns one row per bucket and status, ordered by bucket: fold them into buckets.
	var buckets []Bucket
	for _, row := range rows {
		if n := len(buckets); n == 0 || !buckets[n-1].Start.Equal(row.Bucket) {
			buckets = append(buckets, Bucket{Start: row.Bucket.UTC()})
		}
		buckets[len(buckets)-1].Counts.add(row.Status, row.Total)
	}
	return buckets, nil
}

// add records n deliveries of the given status. Statuses this feature does not know are ignored.
func (c *Counts) add(status string, n int64) {
	switch status {
	case "succeeded":
		c.Succeeded += n
	case "failed":
		c.Failed += n
	case "pending":
		c.Pending += n
	}
}
