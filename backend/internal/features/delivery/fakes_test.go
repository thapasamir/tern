package delivery

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/pagination"
)

// fakeRepo is an in-memory Repository that records how it was used.
type fakeRepo struct {
	work       map[uuid.UUID]Work
	deliveries map[uuid.UUID]Delivery
	attempts   map[uuid.UUID][]Attempt
	unenqueued []uuid.UUID

	getWorkErr   error
	recordErr    error
	notRecorded  bool // RecordAttempt reports a lost race
	markErr      error
	listUnenqErr error

	failPendingErr error
	overdue        []uuid.UUID
	overdueErr     error

	recorded         []AttemptRecord
	failedPending    []failPendingCall
	overdueCutoff    time.Time
	marked           [][]uuid.UUID
	markedAt         []time.Time
	unenqueuedCutoff time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		work:       map[uuid.UUID]Work{},
		deliveries: map[uuid.UUID]Delivery{},
		attempts:   map[uuid.UUID][]Attempt{},
	}
}

func (f *fakeRepo) GetWork(_ context.Context, id uuid.UUID) (Work, error) {
	if f.getWorkErr != nil {
		return Work{}, f.getWorkErr
	}
	w, ok := f.work[id]
	if !ok {
		return Work{}, ErrNotFound
	}
	return w, nil
}

func (f *fakeRepo) RecordAttempt(_ context.Context, rec AttemptRecord) (bool, error) {
	if f.recordErr != nil {
		return false, f.recordErr
	}
	f.recorded = append(f.recorded, rec)
	return !f.notRecorded, nil
}

func (f *fakeRepo) FailPendingForEndpoint(_ context.Context, endpointID uuid.UUID, reason string, _ time.Time) (int64, error) {
	if f.failPendingErr != nil {
		return 0, f.failPendingErr
	}
	f.failedPending = append(f.failedPending, failPendingCall{endpointID, reason})
	return 1, nil
}

func (f *fakeRepo) ListOverdue(_ context.Context, dueBefore time.Time, _ int) ([]uuid.UUID, error) {
	f.overdueCutoff = dueBefore
	return f.overdue, f.overdueErr
}

type failPendingCall struct {
	endpointID uuid.UUID
	reason     string
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

func (f *fakeRepo) ListByEvent(_ context.Context, tenantID, eventID uuid.UUID, p pagination.Params) ([]Delivery, error) {
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

// fakeSender returns a canned result and records the requests it received.
type fakeSender struct {
	result   SendResult
	requests []SendRequest
}

func (f *fakeSender) Send(_ context.Context, req SendRequest) SendResult {
	f.requests = append(f.requests, req)
	return f.result
}

// fakeHealth records what the processor told the endpoint feature and answers as configured.
type fakeHealth struct {
	successes []uuid.UUID
	failures  []healthCall
	disables  []healthCall

	disableOnFailure bool // RecordFailure reports that it disabled the endpoint
	disableOnDisable bool // DisableIfActive reports that it disabled the endpoint
	err              error
}

type healthCall struct {
	endpointID   uuid.UUID
	disableAfter int
	reason       string
}

func (f *fakeHealth) RecordSuccess(_ context.Context, _, endpointID uuid.UUID) error {
	f.successes = append(f.successes, endpointID)
	return f.err
}

func (f *fakeHealth) RecordFailure(_ context.Context, _, endpointID uuid.UUID, disableAfter int) (bool, error) {
	f.failures = append(f.failures, healthCall{endpointID: endpointID, disableAfter: disableAfter})
	return f.disableOnFailure, f.err
}

func (f *fakeHealth) DisableIfActive(_ context.Context, _, endpointID uuid.UUID, reason string) (bool, error) {
	f.disables = append(f.disables, healthCall{endpointID: endpointID, reason: reason})
	return f.disableOnDisable, f.err
}
