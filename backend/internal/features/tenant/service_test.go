package tenant

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/testutil"
)

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	tenants map[uuid.UUID]Tenant
	keys    map[uuid.UUID]*APIKey
	hashes  map[string]uuid.UUID // hex-free: string(hash) -> key id
	touched []uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		tenants: map[uuid.UUID]Tenant{},
		keys:    map[uuid.UUID]*APIKey{},
		hashes:  map[string]uuid.UUID{},
	}
}

func (f *fakeRepo) CreateTenantWithKey(_ context.Context, t Tenant, k APIKey, h []byte) error {
	f.tenants[t.ID] = t
	return f.CreateAPIKey(context.Background(), k, h)
}

func (f *fakeRepo) CreateAPIKey(_ context.Context, k APIKey, h []byte) error {
	if _, ok := f.tenants[k.TenantID]; !ok {
		return ErrNotFound
	}
	f.keys[k.ID] = &k
	f.hashes[string(h)] = k.ID
	return nil
}

func (f *fakeRepo) GetTenant(_ context.Context, id uuid.UUID) (Tenant, error) {
	t, ok := f.tenants[id]
	if !ok {
		return Tenant{}, ErrNotFound
	}
	return t, nil
}

func (f *fakeRepo) ListAPIKeys(_ context.Context, tenantID uuid.UUID) ([]APIKey, error) {
	var out []APIKey
	for _, k := range f.keys {
		if k.TenantID == tenantID {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (f *fakeRepo) RevokeAPIKey(_ context.Context, tenantID, keyID uuid.UUID) error {
	k, ok := f.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return ErrNotFound
	}
	if k.RevokedAt == nil {
		now := time.Now()
		k.RevokedAt = &now
	}
	return nil
}

func (f *fakeRepo) FindActiveKeyByHash(_ context.Context, h []byte) (APIKey, error) {
	id, ok := f.hashes[string(h)]
	if !ok || f.keys[id].RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	return *f.keys[id], nil
}

func (f *fakeRepo) TouchAPIKey(_ context.Context, id uuid.UUID) error {
	f.touched = append(f.touched, id)
	now := time.Now()
	f.keys[id].LastUsedAt = &now
	return nil
}

func TestCreateTenant_ReturnsUsableKeyAndStoresOnlyHash(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	tn, key, err := svc.CreateTenant(context.Background(), "  Acme  ")
	if err != nil {
		t.Fatal(err)
	}
	if tn.Name != "Acme" {
		t.Errorf("name = %q, want trimmed %q", tn.Name, "Acme")
	}
	if !strings.HasPrefix(key.Plain, keyPrefix) {
		t.Errorf("key %q missing prefix %q", key.Plain, keyPrefix)
	}
	if !strings.HasPrefix(key.Plain, key.APIKey.Prefix) {
		t.Errorf("display prefix %q is not a prefix of the key", key.APIKey.Prefix)
	}
	for stored := range repo.hashes {
		if strings.Contains(stored, key.Plain) {
			t.Fatal("plaintext key found in storage")
		}
	}

	got, err := svc.Authenticate(context.Background(), key.Plain)
	if err != nil {
		t.Fatalf("authenticate with fresh key: %v", err)
	}
	if got != tn.ID {
		t.Errorf("authenticated tenant = %s, want %s", got, tn.ID)
	}
}

func TestCreateTenant_RejectsBadNames(t *testing.T) {
	svc := NewService(newFakeRepo())
	for _, name := range []string{"", "   ", strings.Repeat("a", maxNameLen+1)} {
		_, _, err := svc.CreateTenant(context.Background(), name)
		testutil.WantCode(t, err, apperror.CodeInvalid)
	}
}

func TestKeysAreUnique(t *testing.T) {
	svc := NewService(newFakeRepo())
	_, a, _ := svc.CreateTenant(context.Background(), "a")
	_, b, _ := svc.CreateTenant(context.Background(), "b")
	if a.Plain == b.Plain {
		t.Fatal("two generated keys are identical")
	}
	if bytes.Equal(hashKey(a.Plain), hashKey(b.Plain)) {
		t.Fatal("two key hashes are identical")
	}
}

func TestAuthenticate_Failures(t *testing.T) {
	svc := NewService(newFakeRepo())
	tn, key, _ := svc.CreateTenant(context.Background(), "acme")

	cases := map[string]string{
		"empty":        "",
		"no prefix":    "abcdef",
		"unknown key":  keyPrefix + "doesnotexist",
		"wrong secret": key.Plain[:len(key.Plain)-1] + "x",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Authenticate(context.Background(), raw)
			testutil.WantCode(t, err, apperror.CodeUnauthorized)
		})
	}

	// Revoked keys stop working immediately.
	if err := svc.RevokeAPIKey(context.Background(), tn.ID, key.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Authenticate(context.Background(), key.Plain)
	testutil.WantCode(t, err, apperror.CodeUnauthorized)
}

func TestAuthenticate_ThrottlesLastUsedWrites(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	_, key, _ := svc.CreateTenant(context.Background(), "acme")

	for i := 0; i < 3; i++ {
		if _, err := svc.Authenticate(context.Background(), key.Plain); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.touched) != 1 {
		t.Fatalf("last_used_at written %d times for 3 quick requests, want 1", len(repo.touched))
	}

	// After the interval has passed, the next request writes again.
	svc.now = func() time.Time { return time.Now().Add(2 * touchInterval) }
	if _, err := svc.Authenticate(context.Background(), key.Plain); err != nil {
		t.Fatal(err)
	}
	if len(repo.touched) != 2 {
		t.Fatalf("touched %d times after interval elapsed, want 2", len(repo.touched))
	}
}

func TestRevokeAPIKey_IsTenantScopedAndIdempotent(t *testing.T) {
	svc := NewService(newFakeRepo())
	ctx := context.Background()
	a, keyA, _ := svc.CreateTenant(ctx, "a")
	b, _, _ := svc.CreateTenant(ctx, "b")

	// Tenant b cannot revoke tenant a's key; it looks like the key does not exist.
	testutil.WantCode(t, svc.RevokeAPIKey(ctx, b.ID, keyA.APIKey.ID), apperror.CodeNotFound)

	if err := svc.RevokeAPIKey(ctx, a.ID, keyA.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeAPIKey(ctx, a.ID, keyA.APIKey.ID); err != nil {
		t.Fatalf("second revoke should succeed, got %v", err)
	}
}

func TestCreateAPIKey_UnknownTenant(t *testing.T) {
	svc := NewService(newFakeRepo())
	_, err := svc.CreateAPIKey(context.Background(), uuid.New(), "ci")
	testutil.WantCode(t, err, apperror.CodeNotFound)
}

func TestInternalErrorsAreNotLeaked(t *testing.T) {
	svc := NewService(brokenRepo{})
	_, _, err := svc.CreateTenant(context.Background(), "acme")
	testutil.WantCode(t, err, apperror.CodeInternal)
	if strings.Contains(apperror.From(err).Message, "boom") {
		t.Fatal("internal cause leaked into client-facing message")
	}
}

type brokenRepo struct{ *fakeRepo }

func (brokenRepo) CreateTenantWithKey(context.Context, Tenant, APIKey, []byte) error {
	return errors.New("boom: connection refused")
}
