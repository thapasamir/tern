// Package queue is a delayed job queue on Redis with at-least-once delivery.
//
// Jobs are opaque string ids (the payload lives in Postgres, the source of truth). Two sorted sets
// hold them, both scored by a unix-millisecond time:
//
//	ready       job -> time it becomes runnable (now for immediate jobs, later for retries)
//	processing  job -> time its lease expires
//
// A worker Claims due jobs, which atomically moves them from ready to processing with a lease.
// It Acks a job when done, or Retries it to run again later. If a worker dies mid-job nobody
// Acks, the lease runs out, and RecoverExpired puts the job back on ready, so no job is lost.
// The cost is that a job can run twice (a slow worker may still be running it); consumers must
// therefore be idempotent.
package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// claimScript moves up to ARGV[2] due jobs from ready to processing with a lease of ARGV[3] ms.
// KEYS: ready, processing. ARGV: now (ms), limit, lease (ms). Runs atomically inside Redis.
var claimScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
for _, id in ipairs(ids) do
  redis.call('ZREM', KEYS[1], id)
  redis.call('ZADD', KEYS[2], tonumber(ARGV[1]) + tonumber(ARGV[3]), id)
end
return ids
`)

// recoverScript moves every job whose lease expired (score <= ARGV[1]) back to ready, runnable
// immediately. NX keeps the existing schedule if the job somehow is on ready already.
// KEYS: ready, processing. ARGV: now (ms). Returns how many jobs were recovered.
var recoverScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1])
for _, id in ipairs(ids) do
  redis.call('ZREM', KEYS[2], id)
  redis.call('ZADD', KEYS[1], 'NX', ARGV[1], id)
end
return #ids
`)

// retryScript moves a job from processing back to ready, scheduled for ARGV[1] (unix ms). Unlike
// Enqueue it replaces any existing schedule: the caller knows exactly when the job should run next.
// KEYS: ready, processing. ARGV: run-at (ms), job id.
var retryScript = redis.NewScript(`
redis.call('ZREM', KEYS[2], ARGV[2])
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
return 1
`)

// Redis implements the queue on a Redis server.
type Redis struct {
	rdb        *redis.Client
	readyKey   string
	workingKey string
	now        func() time.Time
}

// New returns a queue whose keys are namespaced under name, e.g. "tern:deliveries".
func New(rdb *redis.Client, name string) *Redis {
	return &Redis{
		rdb:        rdb,
		readyKey:   name + ":ready",
		workingKey: name + ":processing",
		now:        time.Now,
	}
}

// Enqueue schedules jobs to become runnable at runAt. It is idempotent: a job that is already
// waiting keeps its existing schedule.
func (q *Redis) Enqueue(ctx context.Context, runAt time.Time, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	members := make([]redis.Z, len(ids))
	for i, id := range ids {
		members[i] = redis.Z{Score: float64(millis(runAt)), Member: id}
	}
	if err := q.rdb.ZAddArgs(ctx, q.readyKey, redis.ZAddArgs{NX: true, Members: members}).Err(); err != nil {
		return fmt.Errorf("queue: enqueue: %w", err)
	}
	return nil
}

// Claim returns up to limit jobs that are due, leasing each for the given duration.
func (q *Redis) Claim(ctx context.Context, limit int, lease time.Duration) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	res, err := claimScript.Run(ctx, q.rdb, []string{q.readyKey, q.workingKey},
		millis(q.now()), limit, lease.Milliseconds()).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("queue: claim: %w", err)
	}
	return res, nil
}

// Ack removes a finished job so its lease is not recovered.
func (q *Redis) Ack(ctx context.Context, id string) error {
	if err := q.rdb.ZRem(ctx, q.workingKey, id).Err(); err != nil {
		return fmt.Errorf("queue: ack: %w", err)
	}
	return nil
}

// Retry gives a claimed job back to the queue to run again at runAt, in one atomic step. The job
// stops being leased, so it is neither lost nor recovered early. Use it instead of Ack when a job
// ran but must run once more later, e.g. a delivery that failed and is due for a retry.
func (q *Redis) Retry(ctx context.Context, id string, runAt time.Time) error {
	if err := retryScript.Run(ctx, q.rdb, []string{q.readyKey, q.workingKey}, millis(runAt), id).Err(); err != nil {
		return fmt.Errorf("queue: retry: %w", err)
	}
	return nil
}

// Depth is a snapshot of how much work the queue holds.
type Depth struct {
	// Due is the number of waiting jobs that are ready to run now.
	Due int64
	// Scheduled is the number of waiting jobs set to run later (retries, throttled deliveries).
	Scheduled int64
	// Processing is the number of jobs a worker has claimed and not finished.
	Processing int64
	// OldestDueAge is how long the longest-waiting due job has been due. It is the queue's lag:
	// near zero when workers keep up, growing when they do not.
	OldestDueAge time.Duration
}

// Depth reports how much work the queue holds.
func (q *Redis) Depth(ctx context.Context) (Depth, error) {
	now := q.now()
	nowMs := millis(now)
	limit := strconvMs(nowMs)

	pipe := q.rdb.Pipeline()
	due := pipe.ZCount(ctx, q.readyKey, "-inf", limit)
	waiting := pipe.ZCard(ctx, q.readyKey)
	processing := pipe.ZCard(ctx, q.workingKey)
	oldest := pipe.ZRangeByScoreWithScores(ctx, q.readyKey, &redis.ZRangeBy{Min: "-inf", Max: limit, Count: 1})
	if _, err := pipe.Exec(ctx); err != nil {
		return Depth{}, fmt.Errorf("queue: depth: %w", err)
	}

	d := Depth{Due: due.Val(), Scheduled: waiting.Val() - due.Val(), Processing: processing.Val()}
	if jobs := oldest.Val(); len(jobs) > 0 {
		if lag := time.Duration(nowMs-int64(jobs[0].Score)) * time.Millisecond; lag > 0 {
			d.OldestDueAge = lag
		}
	}
	return d, nil
}

func strconvMs(ms int64) string { return strconv.FormatInt(ms, 10) }

// RecoverExpired makes jobs whose lease ran out runnable again and returns how many there were.
func (q *Redis) RecoverExpired(ctx context.Context) (int, error) {
	n, err := recoverScript.Run(ctx, q.rdb, []string{q.readyKey, q.workingKey}, millis(q.now())).Int()
	if err != nil {
		return 0, fmt.Errorf("queue: recover expired: %w", err)
	}
	return n, nil
}

func millis(t time.Time) int64 { return t.UnixMilli() }
