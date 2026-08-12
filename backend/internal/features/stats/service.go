package stats

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

const (
	// defaultWindow is the period a report covers when the caller names none.
	defaultWindow = 24 * time.Hour
	// maxWindow bounds how much history one report may aggregate.
	maxWindow = 31 * 24 * time.Hour
)

// Input is a report request as received from the API client. Nil fields take their defaults.
type Input struct {
	EndpointID *uuid.UUID
	Since      *time.Time // default: Until minus 24 hours
	Until      *time.Time // default: now
	Interval   string     // default: hour
}

// Service holds the business rules for reports.
type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

// Report builds a delivery report for the tenant.
func (s *Service) Report(ctx context.Context, tenantID uuid.UUID, in Input) (Report, error) {
	q, err := s.query(tenantID, in)
	if err != nil {
		return Report{}, err
	}

	counts, err := s.repo.CountByStatus(ctx, q)
	if err != nil {
		return Report{}, apperror.Internal(err)
	}
	times, err := s.repo.ResponseTimes(ctx, q)
	if err != nil {
		return Report{}, apperror.Internal(err)
	}
	sparse, err := s.repo.Series(ctx, q)
	if err != nil {
		return Report{}, apperror.Internal(err)
	}

	return Report{
		Since:         q.Since,
		Until:         q.Until,
		Interval:      q.Interval,
		Counts:        counts,
		ResponseTimes: times,
		Series:        fillSeries(sparse, q.Since, q.Until, q.Interval),
	}, nil
}

// query applies the defaults and validates the request.
func (s *Service) query(tenantID uuid.UUID, in Input) (Query, error) {
	until := s.now().UTC()
	if in.Until != nil {
		until = in.Until.UTC()
	}
	since := until.Add(-defaultWindow)
	if in.Since != nil {
		since = in.Since.UTC()
	}

	interval := IntervalHour
	if in.Interval != "" {
		var ok bool
		if interval, ok = ParseInterval(in.Interval); !ok {
			return Query{}, apperror.Invalid("interval must be one of: hour, day")
		}
	}

	switch {
	case !since.Before(until):
		return Query{}, apperror.Invalid("since must be before until")
	case until.Sub(since) > maxWindow:
		return Query{}, apperror.Invalid("the range may cover at most 31 days")
	}
	return Query{TenantID: tenantID, EndpointID: in.EndpointID, Since: since, Until: until, Interval: interval}, nil
}

// fillSeries turns the buckets that have deliveries into one bucket per interval across the whole
// range, with zero counts where nothing happened.
func fillSeries(sparse []Bucket, since, until time.Time, interval Interval) []Bucket {
	byStart := make(map[int64]Counts, len(sparse))
	for _, b := range sparse {
		byStart[b.Start.Unix()] = b.Counts
	}

	var series []Bucket
	for start := floor(since, interval); start.Before(until); start = next(start, interval) {
		series = append(series, Bucket{Start: start, Counts: byStart[start.Unix()]})
	}
	return series
}

// floor returns the start, in UTC, of the interval that contains t.
func floor(t time.Time, interval Interval) time.Time {
	t = t.UTC()
	if interval == IntervalDay {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return t.Truncate(time.Hour)
}

// next returns the start of the interval after the one starting at t.
func next(t time.Time, interval Interval) time.Time {
	if interval == IntervalDay {
		return t.AddDate(0, 0, 1)
	}
	return t.Add(time.Hour)
}
