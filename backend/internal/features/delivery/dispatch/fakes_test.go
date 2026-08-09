package dispatch

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
)

// fakeRepo is an in-memory delivery.Repository for the processor tests. It embeds the interface
// and implements only what the processor uses, so a call to anything else panics on the nil
// interface: a loud sign that the processor started depending on something its tests do not cover.
type fakeRepo struct {
	delivery.Repository

	work map[uuid.UUID]delivery.Work

	getWorkErr     error
	recordErr      error
	notRecorded    bool // RecordAttempt reports a lost race
	failPendingErr error

	recorded      []delivery.AttemptRecord
	failedPending []failPendingCall
}

type failPendingCall struct {
	endpointID uuid.UUID
	reason     string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{work: map[uuid.UUID]delivery.Work{}}
}

func (f *fakeRepo) GetWork(_ context.Context, id uuid.UUID) (delivery.Work, error) {
	if f.getWorkErr != nil {
		return delivery.Work{}, f.getWorkErr
	}
	w, ok := f.work[id]
	if !ok {
		return delivery.Work{}, delivery.ErrNotFound
	}
	return w, nil
}

func (f *fakeRepo) RecordAttempt(_ context.Context, rec delivery.AttemptRecord) (bool, error) {
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

// fakeSender returns a canned result and records the requests it received.
type fakeSender struct {
	result   sender.Result
	requests []sender.Request
}

func (f *fakeSender) Send(_ context.Context, req sender.Request) sender.Result {
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
