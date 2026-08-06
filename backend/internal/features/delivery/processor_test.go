package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/ids"
	"github.com/samirthapa/webhook-platform/internal/shared/signing"
	"github.com/samirthapa/webhook-platform/internal/shared/testutil"
)

var testNow = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

const testAutoDisableAfter = 3

type processorFixture struct {
	proc   *Processor
	repo   *fakeRepo
	sender *fakeSender
	health *fakeHealth
	work   Work
}

// newFixture returns a Processor holding one pending delivery, on its first attempt, to an active
// endpoint. The retry schedule is 1 minute then 5 minutes (three attempts in all) with no jitter.
func newFixture(t *testing.T) *processorFixture {
	t.Helper()
	secret, err := signing.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	work := Work{
		DeliveryID:     ids.New(),
		TenantID:       uuid.New(),
		EndpointID:     ids.New(),
		Status:         StatusPending,
		EventID:        ids.New(),
		EventType:      "invoice.paid",
		Payload:        json.RawMessage(`{"amount": 42}`),
		EventCreatedAt: testNow.Add(-time.Minute),
		EndpointURL:    "https://customer.example/hook",
		EndpointSecret: secret,
		EndpointActive: true,
	}
	repo := newFakeRepo()
	sender := &fakeSender{result: SendResult{StatusCode: 200, Duration: 40 * time.Millisecond}}
	health := &fakeHealth{}
	repo.work[work.DeliveryID] = work

	policy := fixedRand(NewRetryPolicy([]time.Duration{time.Minute, 5 * time.Minute}), 0.5)
	p := NewProcessor(repo, sender, health, ProcessorConfig{Retry: policy, AutoDisableAfter: testAutoDisableAfter})
	p.now = func() time.Time { return testNow }
	p.log = testutil.QuietLogger()
	return &processorFixture{proc: p, repo: repo, sender: sender, health: health, work: work}
}

// setWork changes the delivery the fixture's repository holds.
func (f *processorFixture) setWork(mutate func(*Work)) {
	mutate(&f.work)
	f.repo.work[f.work.DeliveryID] = f.work
}

// process runs the job and returns the retry time it asked for.
func (f *processorFixture) process(t *testing.T) time.Time {
	t.Helper()
	retryAt, err := f.proc.Process(context.Background(), f.work.DeliveryID.String())
	if err != nil {
		t.Fatalf("Process returned %v", err)
	}
	return retryAt
}

// onlyRecord asserts exactly one attempt was recorded and returns it.
func (f *processorFixture) onlyRecord(t *testing.T) AttemptRecord {
	t.Helper()
	if len(f.repo.recorded) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(f.repo.recorded))
	}
	return f.repo.recorded[0]
}

func TestProcess_Success(t *testing.T) {
	f := newFixture(t)
	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("a finished job must not ask to run again, got %v", retryAt)
	}

	rec := f.onlyRecord(t)
	if rec.Outcome != StatusSucceeded || rec.RetryAt != nil || rec.Error != nil || rec.StatusCode == nil || *rec.StatusCode != 200 {
		t.Fatalf("record = %+v, want a succeeded attempt with status 200 and no error", rec)
	}
	if rec.DeliveryID != f.work.DeliveryID || rec.Duration != 40*time.Millisecond || !rec.StartedAt.Equal(testNow) {
		t.Fatalf("record bookkeeping wrong: %+v", rec)
	}
}

func TestProcess_SendsASignedEnvelopeThatACustomerCanVerify(t *testing.T) {
	f := newFixture(t)
	f.process(t)
	if len(f.sender.requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(f.sender.requests))
	}
	req := f.sender.requests[0]

	if req.URL != f.work.EndpointURL {
		t.Errorf("URL = %q, want %q", req.URL, f.work.EndpointURL)
	}
	// Verify exactly as a customer's server would: with the endpoint secret, from the wire bytes.
	if err := signing.Verify(f.work.EndpointSecret, req.Header, req.Body, testNow, time.Minute); err != nil {
		t.Fatalf("customer-side verification failed: %v", err)
	}
	if got := req.Header.Get(signing.HeaderID); got != f.work.EventID.String() {
		t.Errorf("webhook-id = %q, want the event id %q (stable across retries and endpoints)", got, f.work.EventID)
	}

	var body struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		CreatedAt time.Time       `json:"created_at"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body.ID != f.work.EventID.String() || body.Type != "invoice.paid" || !body.CreatedAt.Equal(f.work.EventCreatedAt) {
		t.Fatalf("unexpected envelope: %s", req.Body)
	}
	// The stored payload is `{"amount": 42}`; the wire form is compacted, so the signed bytes are
	// the same however Postgres happens to format its jsonb.
	if string(body.Data) != `{"amount":42}` {
		t.Fatalf("data = %s, want the compacted payload", body.Data)
	}
}

func TestProcess_ASuccessfulAttemptClearsTheEndpointsFailureStreak(t *testing.T) {
	f := newFixture(t)
	f.process(t)
	if len(f.health.successes) != 1 || f.health.successes[0] != f.work.EndpointID || len(f.health.failures) != 0 {
		t.Fatalf("health: successes=%v failures=%v; want one success for the endpoint", f.health.successes, f.health.failures)
	}
}

// ---- retries -------------------------------------------------------------------------------

func TestProcess_AFailedAttemptIsRetriedOnTheSchedule(t *testing.T) {
	cases := []struct {
		name          string
		attemptsSoFar int
		wantDelay     time.Duration
	}{
		{"first attempt fails: first delay", 0, time.Minute},
		{"second attempt fails: second delay", 1, 5 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.setWork(func(w *Work) { w.AttemptCount = c.attemptsSoFar })
			f.sender.result = SendResult{StatusCode: 500, Body: "kaput"}

			retryAt := f.process(t)

			want := testNow.Add(c.wantDelay)
			if !retryAt.Equal(want) {
				t.Fatalf("asked to run again at %v, want %v", retryAt, want)
			}
			rec := f.onlyRecord(t)
			if rec.Outcome != StatusPending || rec.RetryAt == nil || !rec.RetryAt.Equal(want) {
				t.Fatalf("record = %+v, want a pending delivery with its retry at %v", rec, want)
			}
			if rec.Error == nil || *rec.Error != "endpoint responded with HTTP 500" || *rec.StatusCode != 500 || *rec.ResponseBody != "kaput" {
				t.Fatalf("the failed attempt must still be fully logged: %+v", rec)
			}
		})
	}
}

func TestProcess_TheLastAttemptFailsTheDeliveryForGood(t *testing.T) {
	f := newFixture(t)
	f.setWork(func(w *Work) { w.AttemptCount = 2 }) // the schedule allows three attempts in all
	f.sender.result = SendResult{StatusCode: 500}

	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("asked to run again at %v after the last attempt", retryAt)
	}
	rec := f.onlyRecord(t)
	if rec.Outcome != StatusFailed || rec.RetryAt != nil {
		t.Fatalf("record = %+v, want a final failure with no retry", rec)
	}
}

func TestProcess_EveryKindOfFailureIsRetried(t *testing.T) {
	cases := map[string]SendResult{
		"server error":        {StatusCode: 500},
		"bad gateway":         {StatusCode: 502},
		"client error":        {StatusCode: 404},
		"unauthorized":        {StatusCode: 401},
		"redirect":            {StatusCode: 302},
		"timeout":             {Err: errors.New("context deadline exceeded")},
		"connection refused":  {Err: errors.New("dial tcp: connection refused")},
		"blocked destination": {Err: errBlockedAddress},
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.sender.result = result
			if retryAt := f.process(t); retryAt.IsZero() {
				t.Fatal("a transient failure must be retried")
			}
			if rec := f.onlyRecord(t); rec.Outcome != StatusPending {
				t.Fatalf("outcome = %s, want pending", rec.Outcome)
			}
		})
	}
}

func TestProcess_2xxStatusesAreAllSuccessesAndNeverRetried(t *testing.T) {
	for _, status := range []int{200, 201, 202, 204, 299} {
		f := newFixture(t)
		f.sender.result = SendResult{StatusCode: status}
		retryAt := f.process(t)
		if rec := f.onlyRecord(t); rec.Outcome != StatusSucceeded || !retryAt.IsZero() {
			t.Errorf("status %d: outcome %s, retry %v; want succeeded and no retry", status, rec.Outcome, retryAt)
		}
	}
}

func TestProcess_HonoursRetryAfterOnlyForTooBusyResponses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter time.Duration
		wantDelay  time.Duration
	}{
		{"429 asks for longer than the backoff", 429, 10 * time.Minute, 10 * time.Minute},
		{"503 asks for longer than the backoff", 503, 10 * time.Minute, 10 * time.Minute},
		{"429 asks for less than the backoff: we keep our own", 429, 5 * time.Second, time.Minute},
		{"429 asks for an absurd wait: capped", 429, 1000 * time.Hour, defaultMaxRetryAfter},
		{"a 500 with a Retry-After header: ignored", 500, 10 * time.Minute, time.Minute},
		{"a 429 without the header", 429, 0, time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.sender.result = SendResult{StatusCode: c.status, RetryAfter: c.retryAfter}
			retryAt := f.process(t)
			if want := testNow.Add(c.wantDelay); !retryAt.Equal(want) {
				t.Fatalf("retry at %v, want %v", retryAt, want)
			}
		})
	}
}

func TestProcess_NetworkErrorsAreLoggedWithoutAStatusCode(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{Err: errors.New("dial tcp: connection refused")}
	f.process(t)

	rec := f.onlyRecord(t)
	if rec.StatusCode != nil || rec.ResponseBody != nil || rec.Error == nil || !strings.Contains(*rec.Error, "connection refused") {
		t.Fatalf("record = %+v, want the network error and no status code", rec)
	}
}

func TestProcess_LongErrorsAreTruncatedWithoutBreakingUTF8(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{Err: errors.New(strings.Repeat("é", maxErrorLen))} // 2 bytes each
	f.process(t)

	msg := *f.onlyRecord(t).Error
	if len(msg) > maxErrorLen || !utf8.ValidString(msg) {
		t.Fatalf("error is %d bytes, valid UTF-8 = %v; want <= %d and valid", len(msg), utf8.ValidString(msg), maxErrorLen)
	}
}

func TestProcess_NeverRetriesEarly(t *testing.T) {
	f := newFixture(t)
	dueAt := testNow.Add(10 * time.Minute)
	f.setWork(func(w *Work) { w.AttemptCount, w.NextAttemptAt = 1, &dueAt })

	retryAt := f.process(t)
	if !retryAt.Equal(dueAt) {
		t.Fatalf("asked to run again at %v, want the original due time %v", retryAt, dueAt)
	}
	if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 || len(f.health.failures) != 0 {
		t.Fatal("a delivery that is not due yet must not be attempted, recorded or counted")
	}
}

func TestProcess_ADueRetryIsAttemptedNormally(t *testing.T) {
	f := newFixture(t)
	dueAt := testNow.Add(-time.Second)
	f.setWork(func(w *Work) { w.AttemptCount, w.NextAttemptAt = 1, &dueAt })

	f.process(t)
	if len(f.sender.requests) != 1 || f.onlyRecord(t).Outcome != StatusSucceeded {
		t.Fatal("a retry that has come due must be attempted")
	}
}

// ---- 410 Gone and automatic disabling ----------------------------------------------------

func TestProcess_Gone_FailsAtOnceAndDisablesTheEndpoint(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{StatusCode: http.StatusGone}
	f.health.disableOnDisable = true

	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("a 410 must not be retried, got a retry at %v", retryAt)
	}
	if rec := f.onlyRecord(t); rec.Outcome != StatusFailed || rec.RetryAt != nil {
		t.Fatalf("record = %+v, want a final failure", rec)
	}
	if len(f.health.disables) != 1 || f.health.disables[0].reason != reasonGone || f.health.disables[0].endpointID != f.work.EndpointID {
		t.Fatalf("disables = %+v, want the endpoint disabled for being gone", f.health.disables)
	}
	if len(f.health.failures) != 0 {
		t.Fatal("a 410 disables immediately; it must not also be counted as an ordinary failure")
	}
	if len(f.repo.failedPending) != 1 {
		t.Fatalf("pending deliveries failed %d times, want once after the endpoint was disabled", len(f.repo.failedPending))
	}
}

func TestProcess_EachFailedAttemptIsCountedAgainstTheEndpoint(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{StatusCode: 500}
	f.process(t)

	if len(f.health.failures) != 1 || f.health.failures[0].endpointID != f.work.EndpointID ||
		f.health.failures[0].disableAfter != testAutoDisableAfter {
		t.Fatalf("failures = %+v, want one, carrying the configured limit of %d", f.health.failures, testAutoDisableAfter)
	}
	if len(f.repo.failedPending) != 0 {
		t.Fatal("the endpoint was not disabled, so its other deliveries must be left alone")
	}
}

func TestProcess_TheFinalFailureIsCountedToo(t *testing.T) {
	f := newFixture(t)
	f.setWork(func(w *Work) { w.AttemptCount = 2 })
	f.sender.result = SendResult{StatusCode: 500}
	f.process(t)
	if len(f.health.failures) != 1 {
		t.Fatalf("failures = %d, want the exhausted delivery counted too", len(f.health.failures))
	}
}

func TestProcess_WhenTheEndpointGetsDisabledItsPendingDeliveriesAreFailed(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{StatusCode: 500}
	f.health.disableOnFailure = true

	f.process(t)
	if len(f.repo.failedPending) != 1 {
		t.Fatalf("failed pending %d times, want 1", len(f.repo.failedPending))
	}
	call := f.repo.failedPending[0]
	if call.endpointID != f.work.EndpointID || call.reason != "endpoint is disabled" {
		t.Fatalf("failed pending = %+v, want this endpoint with the reason \"endpoint is disabled\"", call)
	}
}

func TestProcess_HealthTrackingIsBestEffort(t *testing.T) {
	t.Run("a failing health update does not fail the job", func(t *testing.T) {
		f := newFixture(t)
		f.sender.result = SendResult{StatusCode: 500}
		f.health.err = errors.New("db down")
		retryAt := f.process(t) // fails the test itself if Process returns an error
		if retryAt.IsZero() {
			t.Fatal("the retry must still be scheduled")
		}
		if len(f.repo.recorded) != 1 {
			t.Fatal("the attempt must stay recorded")
		}
	})
	t.Run("failing to fail the pending deliveries does not fail the job", func(t *testing.T) {
		f := newFixture(t)
		f.sender.result = SendResult{StatusCode: 500}
		f.health.disableOnFailure = true
		f.repo.failPendingErr = errors.New("db down")
		f.process(t)
	})
}

// ---- attempts that cannot be made --------------------------------------------------------

func TestProcess_DisabledEndpointFailsWithoutSendingOrRetrying(t *testing.T) {
	f := newFixture(t)
	f.setWork(func(w *Work) { w.EndpointActive = false })

	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("a disabled endpoint must not be retried, got %v", retryAt)
	}
	if len(f.sender.requests) != 0 {
		t.Fatal("a request was sent to a disabled endpoint")
	}
	rec := f.onlyRecord(t)
	if rec.Outcome != StatusFailed || rec.Error == nil || *rec.Error != "endpoint is disabled" {
		t.Fatalf("record = %+v, want failed with \"endpoint is disabled\"", rec)
	}
	if len(f.health.failures)+len(f.health.successes)+len(f.health.disables) != 0 {
		t.Fatal("an attempt that was never made says nothing about the endpoint's health")
	}
}

func TestProcess_UnusableSecretFailsWithoutSendingOrRetrying(t *testing.T) {
	f := newFixture(t)
	f.setWork(func(w *Work) { w.EndpointSecret = "not-a-valid-secret" })

	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("retrying cannot fix a broken secret, got a retry at %v", retryAt)
	}
	if len(f.sender.requests) != 0 {
		t.Fatal("an unsigned request was sent")
	}
	if rec := f.onlyRecord(t); rec.Outcome != StatusFailed || !strings.Contains(*rec.Error, "sign payload") {
		t.Fatalf("record = %+v, want a signing failure", rec)
	}
	if len(f.health.failures) != 0 {
		t.Fatal("a signing problem must not count against the endpoint")
	}
}

// ---- duplicates, races and infrastructure errors -------------------------------------------

func TestProcess_SkipsDeliveriesThatAreAlreadyDone(t *testing.T) {
	for _, status := range []Status{StatusSucceeded, StatusFailed} {
		f := newFixture(t)
		f.setWork(func(w *Work) { w.Status = status })

		if retryAt := f.process(t); !retryAt.IsZero() {
			t.Errorf("a %s delivery asked to run again", status)
		}
		if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 {
			t.Errorf("a %s delivery was processed again (a duplicate job must be a no-op)", status)
		}
	}
}

func TestProcess_DropsJobsItCannotMakeSenseOf(t *testing.T) {
	f := newFixture(t)
	for _, job := range []string{"not-a-uuid", "", uuid.NewString()} { // malformed, empty, unknown delivery
		retryAt, err := f.proc.Process(context.Background(), job)
		if err != nil || !retryAt.IsZero() {
			t.Errorf("Process(%q) = %v, %v; poison jobs must be acked (zero, nil), not retried forever", job, retryAt, err)
		}
	}
	if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 {
		t.Fatal("nothing should have been sent or recorded")
	}
}

func TestProcess_InfrastructureFailuresAreReturnedSoTheJobIsRedelivered(t *testing.T) {
	t.Run("cannot load the delivery", func(t *testing.T) {
		f := newFixture(t)
		f.repo.getWorkErr = errors.New("db down")
		if _, err := f.proc.Process(context.Background(), f.work.DeliveryID.String()); err == nil {
			t.Fatal("want an error so the job is redelivered")
		}
		if len(f.sender.requests) != 0 {
			t.Fatal("must not send when the delivery could not be loaded")
		}
	})
	t.Run("cannot record the attempt", func(t *testing.T) {
		f := newFixture(t)
		f.repo.recordErr = errors.New("db down")
		if _, err := f.proc.Process(context.Background(), f.work.DeliveryID.String()); err == nil {
			t.Fatal("want an error so the job is redelivered")
		}
		if len(f.health.successes)+len(f.health.failures) != 0 {
			t.Fatal("health must only be updated for attempts that were recorded")
		}
	})
}

func TestProcess_LosingTheRaceToRecordIsNotAnErrorAndLeavesTheFollowUpToTheWinner(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{StatusCode: 500}
	f.repo.notRecorded = true // another worker completed the delivery first

	if retryAt := f.process(t); !retryAt.IsZero() {
		t.Fatalf("a worker that lost the race must not schedule a retry of its own, got %v", retryAt)
	}
	if len(f.health.failures) != 0 {
		t.Fatal("a worker that lost the race must not count the failure a second time")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate(abcdef, 3) = %q", got)
	}
	// "é" is two bytes: cutting at 1 must drop the whole character, not leave half of it.
	if got := truncate("éé", 3); got != "é" || !utf8.ValidString(got) {
		t.Errorf("truncate(éé, 3) = %q, want a whole é", got)
	}
}
