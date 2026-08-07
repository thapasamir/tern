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

func TestListByEvent_IsTenantScopedAndPaginated(t *testing.T) {
	svc, repo, _ := newService()
	tenantID, eventID := uuid.New(), ids.New()
	for i := 0; i < 3; i++ {
		d := Delivery{ID: ids.New(), TenantID: tenantID, EventID: eventID}
		repo.deliveries[d.ID] = d
	}
	other := Delivery{ID: ids.New(), TenantID: uuid.New(), EventID: eventID}
	repo.deliveries[other.ID] = other

	res, err := svc.ListByEvent(context.Background(), tenantID, eventID, pagination.Params{Limit: 2, After: uuid.Max})
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
