package event

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/ids"
)

// fakeRepo is an in-memory Repository. fanOut is how many deliveries each new event produces.
type fakeRepo struct {
	events map[uuid.UUID]Event
	fanOut int
	err    error // returned by Create when set
}

func newFakeRepo(fanOut int) *fakeRepo {
	return &fakeRepo{events: map[uuid.UUID]Event{}, fanOut: fanOut}
}

func (f *fakeRepo) Create(_ context.Context, e Event) (CreateResult, error) {
	if f.err != nil {
		return CreateResult{}, f.err
	}
	if e.IdempotencyKey != nil {
		for _, existing := range f.events {
			if existing.TenantID == e.TenantID && existing.IdempotencyKey != nil && *existing.IdempotencyKey == *e.IdempotencyKey {
				return CreateResult{Created: false}, nil
			}
		}
	}
	f.events[e.ID] = e
	deliveries := make([]uuid.UUID, f.fanOut)
	for i := range deliveries {
		deliveries[i] = ids.New()
	}
	return CreateResult{Created: true, DeliveryIDs: deliveries}, nil
}

func (f *fakeRepo) Get(_ context.Context, tenantID, id uuid.UUID) (Event, error) {
	e, ok := f.events[id]
	if !ok || e.TenantID != tenantID {
		return Event{}, ErrNotFound
	}
	return e, nil
}

func (f *fakeRepo) GetByIdempotencyKey(_ context.Context, tenantID uuid.UUID, key string) (Event, error) {
	for _, e := range f.events {
		if e.TenantID == tenantID && e.IdempotencyKey != nil && *e.IdempotencyKey == key {
			return e, nil
		}
	}
	return Event{}, ErrNotFound
}

// fakeEnqueuer records the delivery ids it was asked to enqueue.
type fakeEnqueuer struct {
	calls [][]uuid.UUID
	err   error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, deliveryIDs []uuid.UUID) error {
	f.calls = append(f.calls, deliveryIDs)
	return f.err
}

func newService(fanOut int) (*Service, *fakeRepo, *fakeEnqueuer) {
	repo, enq := newFakeRepo(fanOut), &fakeEnqueuer{}
	return NewService(repo, enq), repo, enq
}

func wantCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", code)
	}
	if got := apperror.From(err).Code; got != code {
		t.Fatalf("error code = %s, want %s (err: %v)", got, code, err)
	}
}

func input(payload string) IngestInput {
	return IngestInput{Type: "invoice.paid", Payload: json.RawMessage(payload)}
}

func TestIngest_StoresTheEventAndEnqueuesItsDeliveries(t *testing.T) {
	svc, repo, enq := newService(3)
	tenantID := uuid.New()

	res, err := svc.Ingest(context.Background(), tenantID, input(`{"amount":42}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Fatal("a new event must report Created")
	}
	stored, ok := repo.events[res.Event.ID]
	if !ok || stored.TenantID != tenantID || stored.Type != "invoice.paid" || string(stored.Payload) != `{"amount":42}` {
		t.Fatalf("event not stored as sent: %+v", stored)
	}
	if len(enq.calls) != 1 || len(enq.calls[0]) != 3 {
		t.Fatalf("enqueue calls = %v, want one call with the 3 fanned-out deliveries", enq.calls)
	}
}

func TestIngest_WithNoMatchingEndpointsStillSucceedsAndEnqueuesNothing(t *testing.T) {
	svc, repo, enq := newService(0)
	res, err := svc.Ingest(context.Background(), uuid.New(), input(`{}`))
	if err != nil || !res.Created {
		t.Fatalf("Ingest = %+v, %v; want a created event", res, err)
	}
	if len(repo.events) != 1 || len(enq.calls) != 0 {
		t.Fatalf("stored %d events and made %d enqueue calls; want 1 and 0", len(repo.events), len(enq.calls))
	}
}

func TestIngest_QueueFailureDoesNotFailTheRequest(t *testing.T) {
	svc, repo, enq := newService(2)
	enq.err = errors.New("redis: connection refused")

	res, err := svc.Ingest(context.Background(), uuid.New(), input(`{}`))
	if err != nil {
		t.Fatalf("Ingest = %v; the event is committed, so the client must get a success", err)
	}
	if _, ok := repo.events[res.Event.ID]; !ok {
		t.Fatal("event was not stored")
	}
}

func TestIngest_StorageFailureIsAnInternalError(t *testing.T) {
	svc, repo, enq := newService(1)
	repo.err = errors.New("boom: connection refused")

	_, err := svc.Ingest(context.Background(), uuid.New(), input(`{}`))
	wantCode(t, err, apperror.CodeInternal)
	if strings.Contains(apperror.From(err).Message, "boom") {
		t.Fatal("internal cause leaked into the client-facing message")
	}
	if len(enq.calls) != 0 {
		t.Fatal("nothing may be enqueued when the event was not stored")
	}
}

func TestIngest_IdempotentReplayReturnsTheOriginalAndEnqueuesNothingNew(t *testing.T) {
	svc, repo, enq := newService(2)
	tenantID := uuid.New()
	in := input(`{"n":1}`)
	in.IdempotencyKey = "order-123"

	first, err := svc.Ingest(context.Background(), tenantID, in)
	if err != nil || !first.Created {
		t.Fatalf("first send = %+v, %v", first, err)
	}
	second, err := svc.Ingest(context.Background(), tenantID, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created {
		t.Error("a replay must not report Created")
	}
	if second.Event.ID != first.Event.ID {
		t.Errorf("replay returned event %s, want the original %s", second.Event.ID, first.Event.ID)
	}
	if len(repo.events) != 1 || len(enq.calls) != 1 {
		t.Errorf("%d events and %d enqueue calls after a replay; want 1 and 1", len(repo.events), len(enq.calls))
	}
}

func TestIngest_IdempotencyKeysAreScopedPerTenant(t *testing.T) {
	svc, repo, _ := newService(1)
	in := input(`{}`)
	in.IdempotencyKey = "same-key"

	a, _ := svc.Ingest(context.Background(), uuid.New(), in)
	b, _ := svc.Ingest(context.Background(), uuid.New(), in)
	if !a.Created || !b.Created || a.Event.ID == b.Event.ID || len(repo.events) != 2 {
		t.Fatal("two tenants using the same key must each get their own event")
	}
}

func TestIngest_Validation(t *testing.T) {
	longKey := strings.Repeat("k", maxIdempotencyKeyLen+1)
	bigPayload := `{"x":"` + strings.Repeat("a", maxPayloadBytes) + `"}`

	cases := map[string]IngestInput{
		"missing type":                 {Payload: json.RawMessage(`{}`)},
		"malformed type":               {Type: "Invoice Paid", Payload: json.RawMessage(`{}`)},
		"missing payload":              {Type: "a.b"},
		"payload is null":              {Type: "a.b", Payload: json.RawMessage(`null`)},
		"payload is an array":          {Type: "a.b", Payload: json.RawMessage(`[1]`)},
		"payload is a string":          {Type: "a.b", Payload: json.RawMessage(`"x"`)},
		"payload too large":            {Type: "a.b", Payload: json.RawMessage(bigPayload)},
		"idempotency key too long":     {Type: "a.b", Payload: json.RawMessage(`{}`), IdempotencyKey: longKey},
		"idempotency key with newline": {Type: "a.b", Payload: json.RawMessage(`{}`), IdempotencyKey: "a\nb"},
		"idempotency key non-ascii":    {Type: "a.b", Payload: json.RawMessage(`{}`), IdempotencyKey: "clé"},
	}
	svc, repo, enq := newService(1)
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Ingest(context.Background(), uuid.New(), in)
			wantCode(t, err, apperror.CodeInvalid)
		})
	}
	if len(repo.events) != 0 || len(enq.calls) != 0 {
		t.Fatal("rejected requests must not store or enqueue anything")
	}
}

func TestGet_IsTenantScoped(t *testing.T) {
	svc, _, _ := newService(0)
	owner := uuid.New()
	res, _ := svc.Ingest(context.Background(), owner, input(`{}`))

	if _, err := svc.Get(context.Background(), owner, res.Event.ID); err != nil {
		t.Fatalf("owner cannot read own event: %v", err)
	}
	_, err := svc.Get(context.Background(), uuid.New(), res.Event.ID)
	wantCode(t, err, apperror.CodeNotFound)
}

func TestValidateIdempotencyKey_EmptyMeansNone(t *testing.T) {
	key, err := validateIdempotencyKey("")
	if err != nil || key != nil {
		t.Fatalf("validateIdempotencyKey(\"\") = %v, %v; want nil, nil", key, err)
	}
}
