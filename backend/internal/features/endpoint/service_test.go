package endpoint

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
	"github.com/samirthapa/webhook-platform/internal/shared/ptr"
	"github.com/samirthapa/webhook-platform/internal/shared/signing"
	"github.com/samirthapa/webhook-platform/internal/shared/testutil"
)

// fakeRepo is an in-memory, tenant-scoped Repository.
type fakeRepo struct {
	rows map[uuid.UUID]Endpoint
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[uuid.UUID]Endpoint{}} }

// find returns the row only if it belongs to the tenant, like the real queries do.
func (f *fakeRepo) find(tenantID, id uuid.UUID) (Endpoint, error) {
	e, ok := f.rows[id]
	if !ok || e.TenantID != tenantID {
		return Endpoint{}, ErrNotFound
	}
	return e, nil
}

func (f *fakeRepo) Create(_ context.Context, e Endpoint) error {
	f.rows[e.ID] = e
	return nil
}

func (f *fakeRepo) Get(_ context.Context, tenantID, id uuid.UUID) (Endpoint, error) {
	return f.find(tenantID, id)
}

func (f *fakeRepo) List(_ context.Context, tenantID uuid.UUID, p pagination.Params) ([]Endpoint, error) {
	var out []Endpoint
	for _, e := range f.rows {
		if e.TenantID == tenantID && strings.Compare(e.ID.String(), p.After.String()) < 0 {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Endpoint) int { return strings.Compare(b.ID.String(), a.ID.String()) })
	if int32(len(out)) > p.QueryLimit() {
		out = out[:p.QueryLimit()]
	}
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, e Endpoint) error {
	cur, err := f.find(e.TenantID, e.ID)
	if err != nil {
		return err
	}
	cur.URL, cur.Description, cur.EventTypes, cur.UpdatedAt = e.URL, e.Description, e.EventTypes, e.UpdatedAt
	f.rows[e.ID] = cur
	return nil
}

func (f *fakeRepo) UpdateSecret(_ context.Context, tenantID, id uuid.UUID, secret string, at time.Time) error {
	e, err := f.find(tenantID, id)
	if err != nil {
		return err
	}
	e.Secret, e.UpdatedAt = secret, at
	f.rows[id] = e
	return nil
}

func (f *fakeRepo) Disable(_ context.Context, tenantID, id uuid.UUID, reason string, at time.Time) error {
	e, err := f.find(tenantID, id)
	if err != nil {
		return err
	}
	e.Status, e.DisabledReason, e.DisabledAt = StatusDisabled, &reason, &at
	f.rows[id] = e
	return nil
}

func (f *fakeRepo) Enable(_ context.Context, tenantID, id uuid.UUID, _ time.Time) error {
	e, err := f.find(tenantID, id)
	if err != nil {
		return err
	}
	e.Status, e.DisabledReason, e.DisabledAt = StatusActive, nil, nil
	f.rows[id] = e
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, tenantID, id uuid.UUID) error {
	if _, err := f.find(tenantID, id); err != nil {
		return err
	}
	delete(f.rows, id)
	return nil
}

// stubValidator rejects empty URLs and any URL containing "bad", like the real validator would.
type stubValidator struct{}

func (stubValidator) Validate(raw string) error {
	if raw == "" {
		return errors.New("url is required")
	}
	if strings.Contains(raw, "bad") {
		return errors.New("url host is not allowed")
	}
	return nil
}

func newService() *Service { return NewService(newFakeRepo(), stubValidator{}) }

func mustCreate(t *testing.T, svc *Service, tenantID uuid.UUID, in CreateInput) Endpoint {
	t.Helper()
	e, err := svc.Create(context.Background(), tenantID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return e
}

func TestCreate_Defaults(t *testing.T) {
	svc, tenantID := newService(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "  https://example.com/hook  ", Description: " prod "})

	if e.URL != "https://example.com/hook" || e.Description != "prod" {
		t.Errorf("fields not trimmed: url=%q description=%q", e.URL, e.Description)
	}
	if e.Status != StatusActive || e.DisabledAt != nil || e.DisabledReason != nil {
		t.Errorf("new endpoint should be active, got %+v", e)
	}
	if !strings.HasPrefix(e.Secret, signing.SecretPrefix) {
		t.Errorf("secret %q missing prefix", e.Secret)
	}
	if e.EventTypes == nil || len(e.EventTypes) != 0 {
		t.Errorf("event types = %#v, want empty non-nil slice", e.EventTypes)
	}
	if e.TenantID != tenantID {
		t.Errorf("tenant = %s, want %s", e.TenantID, tenantID)
	}
}

func TestCreate_Validation(t *testing.T) {
	svc := newService()
	cases := map[string]CreateInput{
		"url rejected by validator": {URL: "https://bad.example.com"},
		"empty url":                 {URL: "   "},
		"description too long":      {URL: "https://example.com", Description: strings.Repeat("a", maxDescriptionLen+1)},
		"uppercase event type":      {URL: "https://example.com", EventTypes: []string{"Invoice.Paid"}},
		"event type with space":     {URL: "https://example.com", EventTypes: []string{"invoice paid"}},
		"empty event type":          {URL: "https://example.com", EventTypes: []string{""}},
		"too many event types":      {URL: "https://example.com", EventTypes: manyEventTypes(maxEventTypes + 1)},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), uuid.New(), in)
			testutil.WantCode(t, err, apperror.CodeInvalid)
		})
	}
}

func manyEventTypes(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "event." + strings.Repeat("a", i%5+1) + string(rune('a'+i%26))
	}
	return out
}

func TestCreate_NormalizesEventTypes(t *testing.T) {
	svc := newService()
	e := mustCreate(t, svc, uuid.New(), CreateInput{
		URL:        "https://example.com",
		EventTypes: []string{" invoice.paid ", "user.created", "invoice.paid", "order_item-shipped"},
	})
	want := []string{"invoice.paid", "user.created", "order_item-shipped"}
	if !slices.Equal(e.EventTypes, want) {
		t.Fatalf("event types = %v, want %v (trimmed, de-duplicated, order kept)", e.EventTypes, want)
	}
}

func TestEndpointsAreIsolatedBetweenTenants(t *testing.T) {
	svc, ctx := newService(), context.Background()
	owner, other := uuid.New(), uuid.New()
	e := mustCreate(t, svc, owner, CreateInput{URL: "https://example.com"})

	_, err := svc.Get(ctx, other, e.ID)
	testutil.WantCode(t, err, apperror.CodeNotFound)
	_, err = svc.Update(ctx, other, e.ID, UpdateInput{Description: ptr.To("hijack")})
	testutil.WantCode(t, err, apperror.CodeNotFound)
	_, err = svc.RotateSecret(ctx, other, e.ID)
	testutil.WantCode(t, err, apperror.CodeNotFound)
	_, err = svc.Disable(ctx, other, e.ID, "x")
	testutil.WantCode(t, err, apperror.CodeNotFound)
	_, err = svc.Enable(ctx, other, e.ID)
	testutil.WantCode(t, err, apperror.CodeNotFound)
	testutil.WantCode(t, svc.Delete(ctx, other, e.ID), apperror.CodeNotFound)

	// ...and none of those attempts changed anything.
	got, err := svc.Get(ctx, owner, e.ID)
	if err != nil || got.Description != "" || got.Secret != e.Secret || got.Status != StatusActive {
		t.Fatalf("endpoint changed by another tenant: %+v (err %v)", got, err)
	}
}

func TestUpdate_IsPartial(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{
		URL: "https://example.com/a", Description: "old", EventTypes: []string{"a.b"},
	})

	got, err := svc.Update(ctx, tenantID, e.ID, UpdateInput{Description: ptr.To("new")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "new" || got.URL != e.URL || !slices.Equal(got.EventTypes, e.EventTypes) {
		t.Fatalf("only the description should change, got %+v", got)
	}

	// An explicitly empty list clears the filter (all events); nil leaves it alone.
	got, err = svc.Update(ctx, tenantID, e.ID, UpdateInput{EventTypes: &[]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EventTypes) != 0 {
		t.Fatalf("event types = %v, want cleared", got.EventTypes)
	}
}

func TestUpdate_InvalidInputChangesNothing(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com/a", Description: "keep"})

	_, err := svc.Update(ctx, tenantID, e.ID, UpdateInput{
		Description: ptr.To("changed"),
		URL:         ptr.To("https://bad.example.com"),
	})
	testutil.WantCode(t, err, apperror.CodeInvalid)

	got, _ := svc.Get(ctx, tenantID, e.ID)
	if got.Description != "keep" || got.URL != "https://example.com/a" {
		t.Fatalf("a rejected update must not be partially applied, got %+v", got)
	}
}

func TestRotateSecret(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com"})

	rotated, err := svc.RotateSecret(ctx, tenantID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Secret == e.Secret || !strings.HasPrefix(rotated.Secret, signing.SecretPrefix) {
		t.Fatalf("secret not replaced: old=%q new=%q", e.Secret, rotated.Secret)
	}
	stored, _ := svc.Get(ctx, tenantID, e.ID)
	if stored.Secret != rotated.Secret {
		t.Fatal("rotated secret was not persisted")
	}
}

func TestDisableAndEnable(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com"})

	disabled, err := svc.Disable(ctx, tenantID, e.ID, "10 consecutive failures")
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != StatusDisabled || disabled.DisabledAt == nil ||
		disabled.DisabledReason == nil || *disabled.DisabledReason != "10 consecutive failures" {
		t.Fatalf("disable did not record status and reason: %+v", disabled)
	}

	enabled, err := svc.Enable(ctx, tenantID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Status != StatusActive || enabled.DisabledAt != nil || enabled.DisabledReason != nil {
		t.Fatalf("enable did not clear the disabled state: %+v", enabled)
	}
}

func TestDisable_RequiresAReason(t *testing.T) {
	svc, tenantID := newService(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com"})
	_, err := svc.Disable(context.Background(), tenantID, e.ID, "  ")
	testutil.WantCode(t, err, apperror.CodeInternal) // caller bug, not a client error
}

func TestDelete(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	e := mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com"})

	if err := svc.Delete(ctx, tenantID, e.ID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Get(ctx, tenantID, e.ID)
	testutil.WantCode(t, err, apperror.CodeNotFound)
	testutil.WantCode(t, svc.Delete(ctx, tenantID, e.ID), apperror.CodeNotFound)
}

func TestList_PagesThroughEverythingNewestFirstWithoutDuplicates(t *testing.T) {
	svc, ctx, tenantID := newService(), context.Background(), uuid.New()
	var created []uuid.UUID
	for i := 0; i < 5; i++ {
		created = append(created, mustCreate(t, svc, tenantID, CreateInput{URL: "https://example.com"}).ID)
	}
	mustCreate(t, svc, uuid.New(), CreateInput{URL: "https://example.com"}) // another tenant's, must not appear

	var got []uuid.UUID
	params := pagination.Params{Limit: 2, After: uuid.Max}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
		res, err := svc.List(ctx, tenantID, params)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) > 2 {
			t.Fatalf("page has %d items, limit is 2", len(res.Items))
		}
		for _, e := range res.Items {
			got = append(got, e.ID)
		}
		if res.NextCursor == nil {
			break
		}
		params.After = *res.NextCursor
	}

	slices.Reverse(created) // newest first
	if !slices.Equal(got, created) {
		t.Fatalf("paged ids = %v, want %v", got, created)
	}
}

func TestInternalErrorsAreNotLeaked(t *testing.T) {
	svc := NewService(brokenRepo{newFakeRepo()}, stubValidator{})
	_, err := svc.Create(context.Background(), uuid.New(), CreateInput{URL: "https://example.com"})
	testutil.WantCode(t, err, apperror.CodeInternal)
	if strings.Contains(apperror.From(err).Message, "boom") {
		t.Fatal("internal cause leaked into the client-facing message")
	}
}

type brokenRepo struct{ *fakeRepo }

func (brokenRepo) Create(context.Context, Endpoint) error {
	return errors.New("boom: connection refused")
}
