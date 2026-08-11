package delivery

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ids"
	"github.com/samirthapa/tern/internal/shared/pagination"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

func newService() (*Service, *fakeRepo, *fakeQueue) {
	repo, q := newFakeRepo(), &fakeQueue{}
	svc := NewService(repo, q)
	svc.now = func() time.Time { return testNow }
	svc.log = testutil.QuietLogger()
	return svc, repo, q
}

func TestEnqueue_QueuesTheJobsAndRecordsThat(t *testing.T) {
	svc, repo, q := newService()
	a, b := ids.New(), ids.New()

	if err := svc.Enqueue(context.Background(), []uuid.UUID{a, b}); err != nil {
		t.Fatal(err)
	}
	if want := []string{a.String(), b.String()}; !slices.Equal(q.jobs, want) {
		t.Fatalf("queued %v, want %v", q.jobs, want)
	}
	if !q.runAts[0].Equal(testNow) {
		t.Errorf("jobs scheduled for %v, want now (%v)", q.runAts[0], testNow)
	}
	if len(repo.marked) != 1 || !slices.Equal(repo.marked[0], []uuid.UUID{a, b}) || !repo.markedAt[0].Equal(testNow) {
		t.Fatalf("marked %v at %v; want both deliveries marked enqueued at now", repo.marked, repo.markedAt)
	}
}

func TestEnqueue_NothingToDo(t *testing.T) {
	svc, repo, q := newService()
	if err := svc.Enqueue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(q.jobs) != 0 || len(repo.marked) != 0 {
		t.Fatal("an empty enqueue must not touch the queue or the database")
	}
}

func TestEnqueue_DoesNotMarkDeliveriesWhenTheQueueIsDown(t *testing.T) {
	svc, repo, q := newService()
	q.err = errors.New("redis: connection refused")

	if err := svc.Enqueue(context.Background(), []uuid.UUID{ids.New()}); err == nil {
		t.Fatal("want an error when the queue rejects the jobs")
	}
	if len(repo.marked) != 0 {
		t.Fatal("deliveries were marked as enqueued although they were not; the sweeper would never retry them")
	}
}

func TestEnqueue_BookkeepingFailureAfterQueueingIsReported(t *testing.T) {
	svc, repo, q := newService()
	repo.markErr = errors.New("db down")

	if err := svc.Enqueue(context.Background(), []uuid.UUID{ids.New()}); err == nil {
		t.Fatal("want the bookkeeping error")
	}
	if len(q.jobs) != 1 {
		t.Fatal("the jobs should be queued even though the bookkeeping failed")
	}
}

func TestEnqueueOrphans_QueuesDeliveriesThatNeverReachedTheQueue(t *testing.T) {
	svc, repo, q := newService()
	orphans := []uuid.UUID{ids.New(), ids.New()}
	repo.unenqueued = orphans

	if err := svc.EnqueueOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := testNow.Add(-orphanGrace); !repo.unenqueuedCutoff.Equal(want) {
		t.Errorf("looked for orphans older than %v, want %v (so fresh deliveries are left alone)", repo.unenqueuedCutoff, want)
	}
	if len(q.jobs) != 2 || len(repo.marked) != 1 {
		t.Fatalf("queued %d jobs and marked %d batches; want 2 and 1", len(q.jobs), len(repo.marked))
	}
}

func TestEnqueueOrphans_NoOrphansIsQuiet(t *testing.T) {
	svc, _, q := newService()
	if err := svc.EnqueueOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.jobs) != 0 {
		t.Fatal("nothing to sweep, nothing should be queued")
	}
}

func TestEnqueueOrphans_ReportsLookupFailures(t *testing.T) {
	svc, repo, _ := newService()
	repo.listUnenqErr = errors.New("db down")
	if err := svc.EnqueueOrphans(context.Background()); err == nil {
		t.Fatal("want the lookup error")
	}
}

func TestGetWithAttempts(t *testing.T) {
	svc, repo, _ := newService()
	tenantID := uuid.New()
	d := Delivery{ID: ids.New(), TenantID: tenantID, EventID: ids.New(), Status: StatusFailed}
	repo.deliveries[d.ID] = d
	repo.attempts[d.ID] = []Attempt{{Number: 1}, {Number: 2}}

	got, attempts, err := svc.GetWithAttempts(context.Background(), tenantID, d.ID)
	if err != nil || got.ID != d.ID || len(attempts) != 2 {
		t.Fatalf("GetWithAttempts = %+v, %d attempts, %v", got, len(attempts), err)
	}

	_, _, err = svc.GetWithAttempts(context.Background(), uuid.New(), d.ID) // another tenant
	testutil.WantCode(t, err, apperror.CodeNotFound)
	_, _, err = svc.GetWithAttempts(context.Background(), tenantID, uuid.New())
	testutil.WantCode(t, err, apperror.CodeNotFound)
}

func TestList_IsTenantScopedAndPaginated(t *testing.T) {
	svc, repo, _ := newService()
	tenantID, eventID := uuid.New(), ids.New()
	for i := 0; i < 3; i++ {
		d := Delivery{ID: ids.New(), TenantID: tenantID, EventID: eventID}
		repo.deliveries[d.ID] = d
	}
	other := Delivery{ID: ids.New(), TenantID: uuid.New(), EventID: eventID}
	repo.deliveries[other.ID] = other

	res, err := svc.List(context.Background(), tenantID, ListFilter{EventID: &eventID}, pagination.Params{Limit: 2, After: uuid.Max})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || res.NextCursor == nil {
		t.Fatalf("got %d items and cursor %v; want a page of 2 with a next cursor", len(res.Items), res.NextCursor)
	}
	for _, d := range res.Items {
		if d.TenantID != tenantID {
			t.Fatal("another tenant's delivery leaked into the list")
		}
	}
}

func TestReconcileOverdue_RequeuesDeliveriesWhoseJobsWereLost(t *testing.T) {
	svc, repo, q := newService()
	lost := []uuid.UUID{ids.New(), ids.New()}
	repo.overdue = lost

	if err := svc.ReconcileOverdue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := testNow.Add(-overdueGrace); !repo.overdueCutoff.Equal(want) {
		t.Errorf("looked for deliveries overdue since %v, want %v: a queue that is merely a little slow must be left alone", repo.overdueCutoff, want)
	}
	if len(q.jobs) != 2 || len(repo.marked) != 1 {
		t.Fatalf("queued %d jobs and marked %d batches; want 2 and 1", len(q.jobs), len(repo.marked))
	}
	if !q.runAts[0].Equal(testNow) {
		t.Errorf("overdue jobs scheduled for %v, want now", q.runAts[0])
	}
}

func TestReconcileOverdue_NothingOverdueIsQuiet(t *testing.T) {
	svc, _, q := newService()
	if err := svc.ReconcileOverdue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.jobs) != 0 {
		t.Fatal("nothing is overdue, nothing should be queued")
	}
}

func TestReconcileOverdue_ReportsLookupFailures(t *testing.T) {
	svc, repo, _ := newService()
	repo.overdueErr = errors.New("db down")
	if err := svc.ReconcileOverdue(context.Background()); err == nil {
		t.Fatal("want the lookup error")
	}
}

// ---- replay ----------------------------------------------------------------------------------

// replayFixture is a failed delivery to an active endpoint.
type replayFixture struct {
	svc  *Service
	repo *fakeRepo
	q    *fakeQueue
	src  ReplaySource
}

func newReplayFixture(t *testing.T) *replayFixture {
	t.Helper()
	svc, repo, q := newService()
	src := ReplaySource{
		DeliveryID: ids.New(), TenantID: uuid.New(), EventID: ids.New(), EndpointID: ids.New(),
		Status: StatusFailed, EndpointActive: true,
	}
	repo.replaySources[src.DeliveryID] = src
	repo.endpointActive[src.EndpointID] = true
	return &replayFixture{svc: svc, repo: repo, q: q, src: src}
}

func (f *replayFixture) replay() (Delivery, error) {
	return f.svc.Replay(context.Background(), f.src.TenantID, f.src.DeliveryID)
}

func TestReplay_CreatesALinkedPendingDeliveryAndQueuesIt(t *testing.T) {
	f := newReplayFixture(t)
	d, err := f.replay()
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != StatusPending || d.ReplayedFrom == nil || *d.ReplayedFrom != f.src.DeliveryID {
		t.Fatalf("replay = %+v, want a pending delivery linked to the original", d)
	}
	if d.EventID != f.src.EventID || d.EndpointID != f.src.EndpointID || d.TenantID != f.src.TenantID {
		t.Fatalf("replay = %+v, want the same event, endpoint and tenant as the original", d)
	}
	if d.ID == f.src.DeliveryID {
		t.Fatal("a replay is a new delivery: the original must keep its own record")
	}
	if len(f.q.jobs) != 1 || f.q.jobs[0] != d.ID.String() {
		t.Fatalf("queued %v, want just the new delivery %s", f.q.jobs, d.ID)
	}
	if len(f.repo.marked) != 1 {
		t.Fatal("the new delivery must be marked as queued")
	}
}

func TestReplay_AlsoWorksForDeliveriesThatSucceeded(t *testing.T) {
	f := newReplayFixture(t)
	f.src.Status = StatusSucceeded
	f.repo.replaySources[f.src.DeliveryID] = f.src
	if _, err := f.replay(); err != nil {
		t.Fatalf("replaying a successful delivery = %v; a customer may want it resent", err)
	}
}

func TestReplay_RejectsWhatCannotBeReplayed(t *testing.T) {
	t.Run("a delivery that is still pending", func(t *testing.T) {
		f := newReplayFixture(t)
		f.src.Status = StatusPending
		f.repo.replaySources[f.src.DeliveryID] = f.src
		_, err := f.replay()
		testutil.WantCode(t, err, apperror.CodeConflict)
		if len(f.repo.createdReplays) != 0 || len(f.q.jobs) != 0 {
			t.Fatal("nothing may be created or queued for a rejected replay")
		}
	})
	t.Run("a delivery to a disabled endpoint", func(t *testing.T) {
		f := newReplayFixture(t)
		f.src.EndpointActive = false
		f.repo.replaySources[f.src.DeliveryID] = f.src
		_, err := f.replay()
		testutil.WantCode(t, err, apperror.CodeConflict)
		if len(f.repo.createdReplays) != 0 {
			t.Fatal("a replay to a disabled endpoint would only fail again")
		}
	})
	t.Run("a delivery that does not exist", func(t *testing.T) {
		f := newReplayFixture(t)
		_, err := f.svc.Replay(context.Background(), f.src.TenantID, uuid.New())
		testutil.WantCode(t, err, apperror.CodeNotFound)
	})
	t.Run("another tenant's delivery", func(t *testing.T) {
		f := newReplayFixture(t)
		_, err := f.svc.Replay(context.Background(), uuid.New(), f.src.DeliveryID)
		testutil.WantCode(t, err, apperror.CodeNotFound)
	})
}

func TestReplay_AQueueOutageDoesNotFailTheRequest(t *testing.T) {
	f := newReplayFixture(t)
	f.q.err = errors.New("redis: connection refused")
	d, err := f.replay()
	if err != nil || d.ID == uuid.Nil {
		t.Fatalf("Replay = %+v, %v; the replay is stored, so the caller must get a success (the sweeper queues it)", d, err)
	}
}

func TestReplay_StorageFailureIsAnInternalError(t *testing.T) {
	f := newReplayFixture(t)
	f.repo.createReplayErr = errors.New("db down")
	_, err := f.replay()
	testutil.WantCode(t, err, apperror.CodeInternal)
}

type bulkFixture struct {
	svc        *Service
	repo       *fakeRepo
	q          *fakeQueue
	tenantID   uuid.UUID
	endpointID uuid.UUID
	since      time.Time
}

func newBulkFixture() *bulkFixture {
	svc, repo, q := newService()
	f := &bulkFixture{svc: svc, repo: repo, q: q, tenantID: uuid.New(), endpointID: ids.New(), since: testNow.Add(-time.Hour)}
	repo.endpointActive[f.endpointID] = true
	return f
}

func TestReplayFailed_ReplaysAndQueuesWhatTheRepositoryCreated(t *testing.T) {
	f := newBulkFixture()
	f.repo.replayBatch = ReplayBatch{IDs: []uuid.UUID{ids.New(), ids.New(), ids.New()}}

	res, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed != 3 || res.HasMore {
		t.Fatalf("result = %+v, want 3 replayed and nothing more", res)
	}
	if len(f.q.jobs) != 3 {
		t.Fatalf("queued %d jobs, want all 3 new deliveries", len(f.q.jobs))
	}
}

func TestReplayFailed_PassesTheRangeAndLimitOn(t *testing.T) {
	f := newBulkFixture()
	f.svc.bulkLimit = 2
	until := testNow.Add(-time.Minute)

	if _, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, until); err != nil {
		t.Fatal(err)
	}
	a := f.repo.replayFailedArg
	if !a.called || a.tenantID != f.tenantID || a.endpointID != f.endpointID || !a.since.Equal(f.since) || !a.until.Equal(until) || a.limit != 2 {
		t.Fatalf("repository called with %+v, want the tenant, endpoint, range and the limit of 2", a)
	}
}

func TestReplayFailed_UntilDefaultsToNow(t *testing.T) {
	f := newBulkFixture()
	if _, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := f.repo.replayFailedArg.until; !got.Equal(testNow) {
		t.Fatalf("until = %v, want now (%v)", got, testNow)
	}
}

func TestReplayFailed_ReportsThatThereIsMore(t *testing.T) {
	f := newBulkFixture()
	f.repo.replayBatch = ReplayBatch{IDs: []uuid.UUID{ids.New()}, HasMore: true}
	res, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
	if err != nil || !res.HasMore {
		t.Fatalf("result = %+v, %v; the caller must be told to call again", res, err)
	}
}

func TestReplayFailed_NothingToReplayIsAnEmptySuccess(t *testing.T) {
	f := newBulkFixture()
	res, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
	if err != nil || res.Replayed != 0 || res.HasMore {
		t.Fatalf("result = %+v, %v; want an empty success", res, err)
	}
	if len(f.q.jobs) != 0 {
		t.Fatal("nothing was created, so nothing may be queued")
	}
}

func TestReplayFailed_Validation(t *testing.T) {
	f := newBulkFixture()
	ctx := context.Background()

	_, err := f.svc.ReplayFailed(ctx, f.tenantID, f.endpointID, time.Time{}, time.Time{})
	testutil.WantCode(t, err, apperror.CodeInvalid) // since is required

	_, err = f.svc.ReplayFailed(ctx, f.tenantID, f.endpointID, testNow, testNow)
	testutil.WantCode(t, err, apperror.CodeInvalid) // an empty range

	_, err = f.svc.ReplayFailed(ctx, f.tenantID, f.endpointID, testNow.Add(time.Hour), time.Time{})
	testutil.WantCode(t, err, apperror.CodeInvalid) // since in the future, until defaults to now

	if f.repo.replayFailedArg.called {
		t.Fatal("an invalid request must not reach the repository")
	}
}

func TestReplayFailed_EndpointRules(t *testing.T) {
	t.Run("an endpoint that does not exist", func(t *testing.T) {
		f := newBulkFixture()
		_, err := f.svc.ReplayFailed(context.Background(), f.tenantID, uuid.New(), f.since, time.Time{})
		testutil.WantCode(t, err, apperror.CodeNotFound)
	})
	t.Run("a disabled endpoint", func(t *testing.T) {
		f := newBulkFixture()
		f.repo.endpointActive[f.endpointID] = false
		_, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
		testutil.WantCode(t, err, apperror.CodeConflict)
		if f.repo.replayFailedArg.called {
			t.Fatal("must not replay to a disabled endpoint")
		}
	})
}

func TestReplayFailed_AQueueOutageDoesNotFailTheRequest(t *testing.T) {
	f := newBulkFixture()
	f.repo.replayBatch = ReplayBatch{IDs: []uuid.UUID{ids.New()}}
	f.q.err = errors.New("redis: connection refused")
	res, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
	if err != nil || res.Replayed != 1 {
		t.Fatalf("result = %+v, %v; the replays are stored, so this is a success", res, err)
	}
}

func TestReplayFailed_StorageFailureIsAnInternalError(t *testing.T) {
	f := newBulkFixture()
	f.repo.replayFailedErr = errors.New("db down")
	_, err := f.svc.ReplayFailed(context.Background(), f.tenantID, f.endpointID, f.since, time.Time{})
	testutil.WantCode(t, err, apperror.CodeInternal)
}

func TestList_PassesTheFilterToTheRepository(t *testing.T) {
	svc, repo, _ := newService()
	endpoint, event := ids.New(), ids.New()
	status := StatusFailed
	since, until := testNow.Add(-time.Hour), testNow
	want := ListFilter{EndpointID: &endpoint, EventID: &event, Status: &status, Since: &since, Until: &until}

	if _, err := svc.List(context.Background(), uuid.New(), want, pagination.Params{Limit: 10, After: uuid.Max}); err != nil {
		t.Fatal(err)
	}
	got := repo.listFilter
	if *got.EndpointID != endpoint || *got.EventID != event || *got.Status != StatusFailed ||
		!got.Since.Equal(since) || !got.Until.Equal(until) {
		t.Fatalf("repository got %+v, want every filter passed through", got)
	}
}

func TestList_RejectsBadFilters(t *testing.T) {
	svc, repo, _ := newService()
	ctx, params := context.Background(), pagination.Params{Limit: 10, After: uuid.Max}

	bogus := Status("exploded")
	_, err := svc.List(ctx, uuid.New(), ListFilter{Status: &bogus}, params)
	testutil.WantCode(t, err, apperror.CodeInvalid)

	later, earlier := testNow, testNow.Add(-time.Hour)
	_, err = svc.List(ctx, uuid.New(), ListFilter{Since: &later, Until: &earlier}, params)
	testutil.WantCode(t, err, apperror.CodeInvalid)
	_, err = svc.List(ctx, uuid.New(), ListFilter{Since: &later, Until: &later}, params)
	testutil.WantCode(t, err, apperror.CodeInvalid) // an empty range

	if repo.listCalled {
		t.Fatal("an invalid filter must not reach the repository")
	}
}

func TestList_AnEmptyFilterIsFine(t *testing.T) {
	svc, repo, _ := newService()
	if _, err := svc.List(context.Background(), uuid.New(), ListFilter{}, pagination.Params{Limit: 10, After: uuid.Max}); err != nil {
		t.Fatal(err)
	}
	if !repo.listCalled {
		t.Fatal("the repository should have been asked")
	}
}

func TestParseStatus(t *testing.T) {
	for _, ok := range []string{"pending", "succeeded", "failed"} {
		if st, valid := ParseStatus(ok); !valid || string(st) != ok {
			t.Errorf("ParseStatus(%q) = %q, %v; want it accepted", ok, st, valid)
		}
	}
	for _, bad := range []string{"", "Pending", "FAILED", "done", " failed"} {
		if _, valid := ParseStatus(bad); valid {
			t.Errorf("ParseStatus(%q) accepted; want it rejected", bad)
		}
	}
}
