package queue

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var epoch = time.Unix(1_700_000_000, 0)

// newTestQueue returns a queue on an in-memory Redis whose clock the test controls.
func newTestQueue(t *testing.T) (*Redis, *time.Time) {
	t.Helper()
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	now := epoch
	q := New(rdb, "test")
	q.now = func() time.Time { return now }
	return q, &now
}

func mustClaim(t *testing.T, q *Redis, limit int, lease time.Duration) []string {
	t.Helper()
	got, err := q.Claim(context.Background(), limit, lease)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return got
}

func TestClaim_ReturnsOnlyDueJobsInScheduleOrder(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch.Add(2*time.Second), "later")
	_ = q.Enqueue(ctx, epoch.Add(-2*time.Second), "oldest")
	_ = q.Enqueue(ctx, epoch.Add(-1*time.Second), "older")
	_ = q.Enqueue(ctx, epoch.Add(time.Hour), "much-later")

	got := mustClaim(t, q, 10, time.Minute)
	if want := []string{"oldest", "older"}; !slices.Equal(got, want) {
		t.Fatalf("claimed %v, want %v (due jobs, earliest first)", got, want)
	}
}

func TestClaim_HonoursLimitAndNeverHandsOutAJobTwice(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch, "a", "b", "c")

	first := mustClaim(t, q, 2, time.Minute)
	second := mustClaim(t, q, 2, time.Minute)
	third := mustClaim(t, q, 2, time.Minute)

	if len(first) != 2 || len(second) != 1 || len(third) != 0 {
		t.Fatalf("claims returned %d, %d, %d jobs; want 2, 1, 0", len(first), len(second), len(third))
	}
	all := append(first, second...)
	slices.Sort(all)
	if !slices.Equal(all, []string{"a", "b", "c"}) {
		t.Fatalf("claimed %v, want each of a, b, c exactly once", all)
	}
}

func TestClaim_WithNonPositiveLimitDoesNothing(t *testing.T) {
	q, _ := newTestQueue(t)
	_ = q.Enqueue(context.Background(), epoch, "a")
	if got := mustClaim(t, q, 0, time.Minute); len(got) != 0 {
		t.Fatalf("claimed %v with limit 0", got)
	}
	if got := mustClaim(t, q, 1, time.Minute); len(got) != 1 {
		t.Fatal("job should still be waiting after a zero-limit claim")
	}
}

func TestEnqueue_IsIdempotentAndKeepsTheExistingSchedule(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch.Add(time.Hour), "a")
	_ = q.Enqueue(ctx, epoch, "a") // would make it due now if it overwrote the schedule

	if got := mustClaim(t, q, 10, time.Minute); len(got) != 0 {
		t.Fatalf("job re-enqueued for 'now' must not jump the queue, claimed %v", got)
	}
	if err := q.Enqueue(ctx, epoch); err != nil {
		t.Fatalf("enqueue of no ids = %v, want nil", err)
	}
}

func TestAck_RemovesTheLeaseSoTheJobIsNotRecovered(t *testing.T) {
	q, now := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch, "a")
	mustClaim(t, q, 1, time.Minute)

	if err := q.Ack(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	*now = epoch.Add(time.Hour)
	if n, err := q.RecoverExpired(ctx); err != nil || n != 0 {
		t.Fatalf("RecoverExpired = %d, %v; want 0, nil after ack", n, err)
	}
	if got := mustClaim(t, q, 10, time.Minute); len(got) != 0 {
		t.Fatalf("acked job came back: %v", got)
	}
}

func TestRecoverExpired_RequeuesJobsOfDeadWorkers(t *testing.T) {
	q, now := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch, "crashed", "slow")
	mustClaim(t, q, 2, 30*time.Second)

	// Only the lease that has run out is recovered.
	*now = epoch.Add(20 * time.Second)
	if err := q.Ack(ctx, "slow"); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.RecoverExpired(ctx); n != 0 {
		t.Fatalf("recovered %d jobs before any lease expired, want 0", n)
	}

	*now = epoch.Add(31 * time.Second)
	n, err := q.RecoverExpired(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RecoverExpired = %d, %v; want 1, nil (only the un-acked job)", n, err)
	}
	if got := mustClaim(t, q, 10, time.Minute); !slices.Equal(got, []string{"crashed"}) {
		t.Fatalf("claimed %v after recovery, want [crashed]", got)
	}
}

func TestLeasedJobsAreInvisibleToOtherClaimers(t *testing.T) {
	q, now := newTestQueue(t)
	_ = q.Enqueue(context.Background(), epoch, "a")
	mustClaim(t, q, 1, time.Minute)

	*now = epoch.Add(30 * time.Second) // lease still valid
	if got := mustClaim(t, q, 10, time.Minute); len(got) != 0 {
		t.Fatalf("a leased job was handed out again: %v", got)
	}
}

func TestRetry_ReschedulesTheJobAndReleasesItsLease(t *testing.T) {
	q, now := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch, "a")
	mustClaim(t, q, 1, time.Minute)

	if err := q.Retry(ctx, "a", epoch.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Not runnable before its new time...
	*now = epoch.Add(30 * time.Minute)
	if got := mustClaim(t, q, 10, time.Minute); len(got) != 0 {
		t.Fatalf("retried job claimed %v before it was due", got)
	}
	// ...and the old lease no longer exists, so recovery must not resurrect it early.
	*now = epoch.Add(2 * time.Minute)
	if n, err := q.RecoverExpired(ctx); err != nil || n != 0 {
		t.Fatalf("RecoverExpired = %d, %v; a retried job holds no lease", n, err)
	}

	// ...and runnable once it is due.
	*now = epoch.Add(time.Hour)
	if got := mustClaim(t, q, 10, time.Minute); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("claimed %v once due, want [a]", got)
	}
}

func TestRetry_ReplacesAnExistingSchedule(t *testing.T) {
	q, _ := newTestQueue(t)
	ctx := context.Background()
	_ = q.Enqueue(ctx, epoch.Add(time.Hour), "a")

	// Unlike Enqueue, Retry means "run at exactly this time", so it moves the job earlier too.
	if err := q.Retry(ctx, "a", epoch); err != nil {
		t.Fatal(err)
	}
	if got := mustClaim(t, q, 10, time.Minute); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("claimed %v, want [a] after retrying it to now", got)
	}
}
