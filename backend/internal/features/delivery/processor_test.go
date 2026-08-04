package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/ids"
	"github.com/samirthapa/webhook-platform/internal/shared/signing"
)

var testNow = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

type processorFixture struct {
	proc   *Processor
	repo   *fakeRepo
	sender *fakeSender
	work   Work
}

// newFixture returns a Processor holding one pending delivery to an active endpoint.
func newFixture(t *testing.T) *processorFixture {
	t.Helper()
	secret, err := signing.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	work := Work{
		DeliveryID:     ids.New(),
		TenantID:       uuid.New(),
		Status:         StatusPending,
		EventID:        ids.New(),
		EventType:      "invoice.paid",
		Payload:        json.RawMessage(`{"amount": 42}`),
		EventCreatedAt: testNow.Add(-time.Minute),
		EndpointURL:    "https://customer.example/hook",
		EndpointSecret: secret,
		EndpointActive: true,
	}
	repo, sender := newFakeRepo(), &fakeSender{result: SendResult{StatusCode: 200, Duration: 40 * time.Millisecond}}
	repo.work[work.DeliveryID] = work

	p := NewProcessor(repo, sender)
	p.now = func() time.Time { return testNow }
	p.log = quietLogger()
	return &processorFixture{proc: p, repo: repo, sender: sender, work: work}
}

func (f *processorFixture) process(t *testing.T) error {
	t.Helper()
	return f.proc.Process(context.Background(), f.work.DeliveryID.String())
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
	if err := f.process(t); err != nil {
		t.Fatal(err)
	}

	rec := f.onlyRecord(t)
	if rec.Outcome != StatusSucceeded || rec.Error != nil || rec.StatusCode == nil || *rec.StatusCode != 200 {
		t.Fatalf("record = %+v, want a succeeded attempt with status 200 and no error", rec)
	}
	if rec.DeliveryID != f.work.DeliveryID || rec.Duration != 40*time.Millisecond || !rec.StartedAt.Equal(testNow) {
		t.Fatalf("record bookkeeping wrong: %+v", rec)
	}
}

func TestProcess_SendsASignedEnvelopeThatACustomerCanVerify(t *testing.T) {
	f := newFixture(t)
	if err := f.process(t); err != nil {
		t.Fatal(err)
	}
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
	if body.ID != f.work.EventID.String() || body.Type != "invoice.paid" ||
		!body.CreatedAt.Equal(f.work.EventCreatedAt) {
		t.Fatalf("unexpected envelope: %s", req.Body)
	}
	// The stored payload is `{"amount": 42}`; the wire form is compacted, so the signed bytes are
	// the same however Postgres happens to format its jsonb.
	if string(body.Data) != `{"amount":42}` {
		t.Fatalf("data = %s, want the compacted payload", body.Data)
	}
}

func TestProcess_Non2xxResponsesAreFailures(t *testing.T) {
	cases := map[string]struct {
		status  int
		wantErr string
	}{
		"server error":   {500, "endpoint responded with HTTP 500"},
		"client error":   {404, "endpoint responded with HTTP 404"},
		"redirect":       {302, "endpoint responded with HTTP 302"},
		"just below 200": {199, "endpoint responded with HTTP 199"},
		"just above 299": {300, "endpoint responded with HTTP 300"},
		"rate limited":   {429, "endpoint responded with HTTP 429"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.sender.result = SendResult{StatusCode: c.status, Body: "nope"}
			if err := f.process(t); err != nil {
				t.Fatalf("an unhappy endpoint is a recorded outcome, not an error: %v", err)
			}
			rec := f.onlyRecord(t)
			if rec.Outcome != StatusFailed || rec.Error == nil || *rec.Error != c.wantErr {
				t.Fatalf("record = %+v, want failed with %q", rec, c.wantErr)
			}
			if rec.StatusCode == nil || *rec.StatusCode != c.status {
				t.Errorf("status code = %v, want %d", rec.StatusCode, c.status)
			}
			if rec.ResponseBody == nil || *rec.ResponseBody != "nope" {
				t.Errorf("response body not kept: %v", rec.ResponseBody)
			}
		})
	}
}

func TestProcess_2xxStatusesAreAllSuccesses(t *testing.T) {
	for _, status := range []int{200, 201, 202, 204, 299} {
		f := newFixture(t)
		f.sender.result = SendResult{StatusCode: status}
		_ = f.process(t)
		if rec := f.onlyRecord(t); rec.Outcome != StatusSucceeded {
			t.Errorf("status %d recorded as %s, want succeeded", status, rec.Outcome)
		}
	}
}

func TestProcess_NetworkErrorIsAFailureWithoutAStatusCode(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{Err: errors.New("dial tcp: connection refused")}
	if err := f.process(t); err != nil {
		t.Fatal(err)
	}
	rec := f.onlyRecord(t)
	if rec.Outcome != StatusFailed || rec.StatusCode != nil || rec.ResponseBody != nil ||
		rec.Error == nil || !strings.Contains(*rec.Error, "connection refused") {
		t.Fatalf("record = %+v, want a failure carrying the network error and no status code", rec)
	}
}

func TestProcess_LongErrorsAreTruncatedWithoutBreakingUTF8(t *testing.T) {
	f := newFixture(t)
	f.sender.result = SendResult{Err: errors.New(strings.Repeat("é", maxErrorLen))} // 2 bytes each
	_ = f.process(t)

	msg := *f.onlyRecord(t).Error
	if len(msg) > maxErrorLen || !utf8.ValidString(msg) {
		t.Fatalf("error is %d bytes, valid UTF-8 = %v; want <= %d and valid", len(msg), utf8.ValidString(msg), maxErrorLen)
	}
}

func TestProcess_DisabledEndpointFailsWithoutSending(t *testing.T) {
	f := newFixture(t)
	f.work.EndpointActive = false
	f.repo.work[f.work.DeliveryID] = f.work

	if err := f.process(t); err != nil {
		t.Fatal(err)
	}
	if len(f.sender.requests) != 0 {
		t.Fatal("a request was sent to a disabled endpoint")
	}
	rec := f.onlyRecord(t)
	if rec.Outcome != StatusFailed || rec.Error == nil || *rec.Error != "endpoint is disabled" {
		t.Fatalf("record = %+v, want failed with \"endpoint is disabled\"", rec)
	}
}

func TestProcess_UnusableSecretFailsWithoutSending(t *testing.T) {
	f := newFixture(t)
	f.work.EndpointSecret = "not-a-valid-secret"
	f.repo.work[f.work.DeliveryID] = f.work

	if err := f.process(t); err != nil {
		t.Fatal(err)
	}
	if len(f.sender.requests) != 0 {
		t.Fatal("an unsigned request was sent")
	}
	if rec := f.onlyRecord(t); rec.Outcome != StatusFailed || !strings.Contains(*rec.Error, "sign payload") {
		t.Fatalf("record = %+v, want a signing failure", rec)
	}
}

func TestProcess_SkipsDeliveriesThatAreAlreadyDone(t *testing.T) {
	for _, status := range []Status{StatusSucceeded, StatusFailed} {
		f := newFixture(t)
		f.work.Status = status
		f.repo.work[f.work.DeliveryID] = f.work

		if err := f.process(t); err != nil {
			t.Fatal(err)
		}
		if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 {
			t.Errorf("a %s delivery was processed again (a duplicate job must be a no-op)", status)
		}
	}
}

func TestProcess_DropsJobsItCannotMakeSenseOf(t *testing.T) {
	f := newFixture(t)
	for _, job := range []string{"not-a-uuid", "", uuid.NewString()} { // malformed, empty, unknown delivery
		if err := f.proc.Process(context.Background(), job); err != nil {
			t.Errorf("Process(%q) = %v; poison jobs must be acked (nil), not retried forever", job, err)
		}
	}
	if len(f.sender.requests) != 0 || len(f.repo.recorded) != 0 {
		t.Fatal("nothing should have been sent or recorded")
	}
}

func TestProcess_InfrastructureFailuresAreReturnedSoTheJobIsRetried(t *testing.T) {
	t.Run("cannot load the delivery", func(t *testing.T) {
		f := newFixture(t)
		f.repo.getWorkErr = errors.New("db down")
		if err := f.process(t); err == nil {
			t.Fatal("want an error so the job is redelivered")
		}
		if len(f.sender.requests) != 0 {
			t.Fatal("must not send when the delivery could not be loaded")
		}
	})
	t.Run("cannot record the attempt", func(t *testing.T) {
		f := newFixture(t)
		f.repo.recordErr = errors.New("db down")
		if err := f.process(t); err == nil {
			t.Fatal("want an error so the job is redelivered")
		}
	})
}

func TestProcess_LosingTheRaceToRecordIsNotAnError(t *testing.T) {
	f := newFixture(t)
	f.repo.notRecorded = true // another worker completed the delivery first
	if err := f.process(t); err != nil {
		t.Fatalf("Process = %v, want nil: the delivery is done, the job should be acked", err)
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
