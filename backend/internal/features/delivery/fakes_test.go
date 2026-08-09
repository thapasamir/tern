package delivery

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/pagination"
)

var testNow = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// fakeRepo is an in-memory Repository for the service tests. It embeds the interface and
// implements only what those tests use, so a call to anything else panics on the nil interface:
// a loud sign that the service started depending on something its tests do not cover.
type fakeRepo struct {
	Repository

	deliveries map[uuid.UUID]Delivery
	attempts   map[uuid.UUID][]Attempt
	unenqueued []uuid.UUID
	overdue    []uuid.UUID

	markErr      error
	listUnenqErr error
	overdueErr   error

	marked           [][]uuid.UUID
	markedAt         []time.Time
	unenqueuedCutoff time.Time
	overdueCutoff    time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		deliveries: map[uuid.UUID]Delivery{},
		attempts:   map[uuid.UUID][]Attempt{},
	}
}

func (f *fakeRepo) Get(_ context.Context, tenantID, id uuid.UUID) (Delivery, error) {
	d, ok := f.deliveries[id]
	if !ok || d.TenantID != tenantID {
		return Delivery{}, ErrNotFound
	}
	return d, nil
}

func (f *fakeRepo) ListAttempts(_ context.Context, deliveryID uuid.UUID) ([]Attempt, error) {
	return f.attempts[deliveryID], nil
}

func (f *fakeRepo) ListByEvent(_ context.Context, tenantID, eventID uuid.UUID, _ pagination.Params) ([]Delivery, error) {
	var out []Delivery
	for _, d := range f.deliveries {
		if d.TenantID == tenantID && d.EventID == eventID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListUnenqueued(_ context.Context, olderThan time.Time, _ int) ([]uuid.UUID, error) {
	f.unenqueuedCutoff = olderThan
	return f.unenqueued, f.listUnenqErr
}

func (f *fakeRepo) ListOverdue(_ context.Context, dueBefore time.Time, _ int) ([]uuid.UUID, error) {
	f.overdueCutoff = dueBefore
	return f.overdue, f.overdueErr
}

func (f *fakeRepo) MarkEnqueued(_ context.Context, ids []uuid.UUID, at time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, ids)
	f.markedAt = append(f.markedAt, at)
	return nil
}

// fakeQueue records enqueued job ids.
type fakeQueue struct {
	mu     sync.Mutex
	jobs   []string
	runAts []time.Time
	err    error
}

func (f *fakeQueue) Enqueue(_ context.Context, runAt time.Time, jobIDs ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, jobIDs...)
	f.runAts = append(f.runAts, runAt)
	return nil
}
