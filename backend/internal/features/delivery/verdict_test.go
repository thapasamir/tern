package delivery

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestJudge is the decision table of the delivery pipeline: what each kind of result means.
func TestJudge(t *testing.T) {
	// Schedule 1m, 5m: three attempts in all. No jitter, so retry times are exact.
	p := &Processor{cfg: ProcessorConfig{Retry: fixedRand(NewRetryPolicy([]time.Duration{time.Minute, 5 * time.Minute}), 0.5)}}
	now := testNow

	send := func(status int) attemptResult { return attemptResult{SendResult: SendResult{StatusCode: status}} }
	netErr := attemptResult{SendResult: SendResult{Err: errors.New("connection refused")}}

	cases := []struct {
		name         string
		attemptsMade int
		result       attemptResult
		want         verdict
	}{
		// Successes and permanent answers
		{"2xx succeeds", 0, send(200), verdict{outcome: StatusSucceeded, health: healthSuccess}},
		{"2xx succeeds on a late attempt too", 2, send(204), verdict{outcome: StatusSucceeded, health: healthSuccess}},
		{"410 fails at once, however many attempts are left", 0, send(http.StatusGone), verdict{outcome: StatusFailed, health: healthGone}},

		// Attempts that could not be made: nothing to retry, nothing to say about the endpoint
		{"a skipped attempt fails and does not touch health", 0, skipped(errEndpointDisabled), verdict{outcome: StatusFailed}},

		// Failures worth retrying, while attempts remain
		{"a 500 is retried after the first delay", 0, send(500), verdict{outcome: StatusPending, retryAt: now.Add(time.Minute), health: healthFailure}},
		{"and after the second delay", 1, send(500), verdict{outcome: StatusPending, retryAt: now.Add(5 * time.Minute), health: healthFailure}},
		{"a network error is retried", 0, netErr, verdict{outcome: StatusPending, retryAt: now.Add(time.Minute), health: healthFailure}},
		{"a 404 is retried too", 0, send(404), verdict{outcome: StatusPending, retryAt: now.Add(time.Minute), health: healthFailure}},

		// Giving up
		{"the last attempt failing ends the delivery", 2, send(500), verdict{outcome: StatusFailed, health: healthFailure}},
		{"a network error on the last attempt ends it too", 2, netErr, verdict{outcome: StatusFailed, health: healthFailure}},

		// Retry-After applies to "too busy" statuses only
		{"429 with Retry-After waits as long as asked", 0,
			attemptResult{SendResult: SendResult{StatusCode: 429, RetryAfter: 20 * time.Minute}},
			verdict{outcome: StatusPending, retryAt: now.Add(20 * time.Minute), health: healthFailure}},
		{"503 with Retry-After waits as long as asked", 0,
			attemptResult{SendResult: SendResult{StatusCode: 503, RetryAfter: 20 * time.Minute}},
			verdict{outcome: StatusPending, retryAt: now.Add(20 * time.Minute), health: healthFailure}},
		{"a 500 with Retry-After ignores it", 0,
			attemptResult{SendResult: SendResult{StatusCode: 500, RetryAfter: 20 * time.Minute}},
			verdict{outcome: StatusPending, retryAt: now.Add(time.Minute), health: healthFailure}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := p.judge(Work{AttemptCount: c.attemptsMade}, c.result, now)
			if got != c.want {
				t.Fatalf("judge = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestIsSuccess(t *testing.T) {
	for status, want := range map[int]bool{0: false, 199: false, 200: true, 204: true, 299: true, 300: false, 404: false, 500: false} {
		if got := isSuccess(status); got != want {
			t.Errorf("isSuccess(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestBuildRequest_SignsEachAttemptAnew(t *testing.T) {
	f := newFixture(t)
	first, err := buildRequest(f.work, testNow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildRequest(f.work, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if string(first.Body) != string(second.Body) {
		t.Error("the body must be identical across attempts: it is the same event")
	}
	if first.Header.Get("Webhook-Id") != second.Header.Get("Webhook-Id") {
		t.Error("webhook-id must be stable across attempts, so receivers can de-duplicate")
	}
	if first.Header.Get("Webhook-Signature") == second.Header.Get("Webhook-Signature") {
		t.Error("each attempt must be signed with its own timestamp")
	}
}
