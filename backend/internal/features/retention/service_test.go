package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/shared/testutil"
)

var testNow = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// fakeRepo deletes from a pool of old events and records each call.
type fakeRepo struct {
	remaining int64 // events that are old enough to delete
	cutoffs   []time.Time
	limits    []int
	failAfter int // fail on this call number (1-based); zero means never
	err       error
}

func (f *fakeRepo) DeleteEventsBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	if f.failAfter > 0 && len(f.cutoffs) == f.failAfter {
		return 0, f.err
	}
	n := min(f.remaining, int64(limit))
	f.remaining -= n
	return n, nil
}

func newService(repo *fakeRepo, retention time.Duration) *Service {
	s := NewService(repo, retention)
	s.now = func() time.Time { return testNow }
	s.log = testutil.QuietLogger()
	return s
}

func TestPurge_UsesTheRetentionPeriodAsTheCutoff(t *testing.T) {
	repo := &fakeRepo{remaining: 5}
	svc := newService(repo, 30*24*time.Hour)

	deleted, err := svc.Purge(context.Background())
	if err != nil || deleted != 5 {
		t.Fatalf("Purge = %d, %v; want 5, nil", deleted, err)
	}
	if want := testNow.Add(-30 * 24 * time.Hour); !repo.cutoffs[0].Equal(want) {
		t.Fatalf("cutoff = %v, want %v (30 days ago)", repo.cutoffs[0], want)
	}
}

func TestPurge_StopsAtTheFirstShortBatch(t *testing.T) {
	repo := &fakeRepo{remaining: 2*batchSize + 10}
	deleted, err := newService(repo, time.Hour).Purge(context.Background())
	if err != nil || deleted != 2*batchSize+10 {
		t.Fatalf("Purge = %d, %v; want everything deleted", deleted, err)
	}
	if len(repo.cutoffs) != 3 {
		t.Fatalf("%d delete calls, want 3 (two full batches and a short one)", len(repo.cutoffs))
	}
	for _, l := range repo.limits {
		if l != batchSize {
			t.Fatalf("batch limit %d, want %d: batches keep each transaction short", l, batchSize)
		}
	}
}

func TestPurge_NothingToDeleteIsASingleCall(t *testing.T) {
	repo := &fakeRepo{}
	deleted, err := newService(repo, time.Hour).Purge(context.Background())
	if err != nil || deleted != 0 || len(repo.cutoffs) != 1 {
		t.Fatalf("Purge = %d, %v after %d calls; want 0, nil after exactly one", deleted, err, len(repo.cutoffs))
	}
}

func TestPurge_AHugeBacklogIsWorkedOffOverSeveralRuns(t *testing.T) {
	repo := &fakeRepo{remaining: int64(maxBatchesPerRun)*batchSize + 500}
	svc := newService(repo, time.Hour)

	deleted, err := svc.Purge(context.Background())
	if err != nil || deleted != int64(maxBatchesPerRun)*batchSize {
		t.Fatalf("first run deleted %d (%v), want exactly the per-run cap", deleted, err)
	}
	if len(repo.cutoffs) != maxBatchesPerRun {
		t.Fatalf("%d batches in one run, want at most %d", len(repo.cutoffs), maxBatchesPerRun)
	}

	deleted, _ = svc.Purge(context.Background())
	if deleted != 500 || repo.remaining != 0 {
		t.Fatalf("second run deleted %d, %d left; want the remaining 500", deleted, repo.remaining)
	}
}

func TestPurge_ZeroRetentionKeepsEverything(t *testing.T) {
	for _, retention := range []time.Duration{0, -time.Hour} {
		repo := &fakeRepo{remaining: 100}
		deleted, err := newService(repo, retention).Purge(context.Background())
		if err != nil || deleted != 0 || len(repo.cutoffs) != 0 {
			t.Fatalf("retention %v: Purge = %d, %v with %d calls; want it to do nothing", retention, deleted, err, len(repo.cutoffs))
		}
	}
}

func TestPurge_AnErrorStopsTheRunButReportsWhatWasDeleted(t *testing.T) {
	repo := &fakeRepo{remaining: 5 * batchSize, failAfter: 3, err: errors.New("db down")}
	deleted, err := newService(repo, time.Hour).Purge(context.Background())
	if err == nil {
		t.Fatal("want the error")
	}
	if deleted != 2*batchSize || len(repo.cutoffs) != 3 {
		t.Fatalf("deleted %d after %d calls; want the 2 batches that succeeded reported, and no further calls", deleted, len(repo.cutoffs))
	}
}

func TestPurge_StopsWhenTheContextIsCancelled(t *testing.T) {
	repo := &fakeRepo{remaining: 10 * batchSize}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newService(repo, time.Hour).Purge(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(repo.cutoffs) != 0 {
		t.Fatal("a cancelled purge must not start another batch")
	}
}
