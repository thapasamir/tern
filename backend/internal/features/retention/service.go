// Package retention deletes events, and with them their deliveries and attempts, once they are
// older than the configured retention period, so the database does not grow without bound.
package retention

import (
	"context"
	"log/slog"
	"time"
)

const (
	// batchSize is how many events one delete removes. Small enough that a batch is a short
	// transaction.
	batchSize = 1000
	// maxBatchesPerRun bounds one purge, so a very large backlog is worked off over several runs
	// instead of one long one.
	maxBatchesPerRun = 100
)

// Service purges old events.
type Service struct {
	repo      Repository
	retention time.Duration
	now       func() time.Time
	log       *slog.Logger
}

// NewService returns a Service that keeps events for the given period. A period of zero or less
// keeps everything: Purge then does nothing.
func NewService(repo Repository, retention time.Duration) *Service {
	return &Service{repo: repo, retention: retention, now: time.Now, log: slog.Default()}
}

// Purge deletes events older than the retention period and returns how many it deleted. It works
// in batches and stops at the first one that comes back short, which means nothing is left, or
// after a fixed number of batches, in which case the next run carries on.
func (s *Service) Purge(ctx context.Context) (int64, error) {
	if s.retention <= 0 {
		return 0, nil
	}
	cutoff := s.now().Add(-s.retention)

	var total int64
	for batch := 0; batch < maxBatchesPerRun; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := s.repo.DeleteEventsBefore(ctx, cutoff, batchSize)
		total += n
		if err != nil {
			return total, err
		}
		if n < batchSize {
			break
		}
	}

	if total > 0 {
		s.log.Info("purged events past their retention period", "deleted", total, "older_than", cutoff)
	}
	return total, nil
}
