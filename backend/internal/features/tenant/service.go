package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ids"
	"github.com/samirthapa/tern/internal/shared/validate"
)

const (
	keyPrefix       = "tern_"
	displayPrefixN  = 6 // secret characters shown after "tern_" in listings
	maxNameLen      = 100
	touchInterval   = time.Minute // throttle for last_used_at writes
	secretByteCount = 32
)

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// CreatedKey carries a freshly created key together with its one-time plaintext value.
type CreatedKey struct {
	APIKey APIKey
	Plain  string
}

// CreateTenant creates a tenant and its first API key atomically.
func (s *Service) CreateTenant(ctx context.Context, name string) (Tenant, CreatedKey, error) {
	name, err := validate.Text("name", name, maxNameLen, true)
	if err != nil {
		return Tenant{}, CreatedKey{}, err
	}
	now := s.now().UTC()
	t := Tenant{ID: ids.New(), Name: name, CreatedAt: now}

	k, plain, hash, err := s.newKey(t.ID, "default", now)
	if err != nil {
		return Tenant{}, CreatedKey{}, err
	}
	if err := s.repo.CreateTenantWithKey(ctx, t, k, hash); err != nil {
		return Tenant{}, CreatedKey{}, apperror.Internal(err)
	}
	return t, CreatedKey{APIKey: k, Plain: plain}, nil
}

func (s *Service) CreateAPIKey(ctx context.Context, tenantID uuid.UUID, name string) (CreatedKey, error) {
	name, err := validate.Text("name", name, maxNameLen, true)
	if err != nil {
		return CreatedKey{}, err
	}
	k, plain, hash, err := s.newKey(tenantID, name, s.now().UTC())
	if err != nil {
		return CreatedKey{}, err
	}
	if err := s.repo.CreateAPIKey(ctx, k, hash); err != nil {
		return CreatedKey{}, apperror.FromStore(err, "tenant not found")
	}
	return CreatedKey{APIKey: k, Plain: plain}, nil
}

func (s *Service) GetTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	t, err := s.repo.GetTenant(ctx, id)
	if err != nil {
		return Tenant{}, apperror.FromStore(err, "tenant not found")
	}
	return t, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]APIKey, error) {
	keys, err := s.repo.ListAPIKeys(ctx, tenantID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return keys, nil
}

func (s *Service) RevokeAPIKey(ctx context.Context, tenantID, keyID uuid.UUID) error {
	if err := s.repo.RevokeAPIKey(ctx, tenantID, keyID); err != nil {
		return apperror.FromStore(err, "api key not found")
	}
	return nil
}

// Authenticate resolves a plaintext API key to the tenant that owns it.
// Every failure mode returns the same Unauthorized error so callers learn nothing.
func (s *Service) Authenticate(ctx context.Context, rawKey string) (uuid.UUID, error) {
	invalid := apperror.Unauthorized("invalid or missing API key")
	if !strings.HasPrefix(rawKey, keyPrefix) {
		return uuid.Nil, invalid
	}
	k, err := s.repo.FindActiveKeyByHash(ctx, hashKey(rawKey))
	if errors.Is(err, ErrNotFound) {
		return uuid.Nil, invalid
	}
	if err != nil {
		return uuid.Nil, apperror.Internal(err)
	}

	// Best effort and throttled: a failed bookkeeping write must not fail the request.
	if k.LastUsedAt == nil || s.now().Sub(*k.LastUsedAt) > touchInterval {
		if err := s.repo.TouchAPIKey(ctx, k.ID); err != nil {
			slog.Warn("touch api key failed", "key_id", k.ID, "error", err)
		}
	}
	return k.TenantID, nil
}

func (s *Service) newKey(tenantID uuid.UUID, name string, now time.Time) (APIKey, string, []byte, error) {
	secret := make([]byte, secretByteCount)
	if _, err := rand.Read(secret); err != nil {
		return APIKey{}, "", nil, apperror.Internal(fmt.Errorf("generate key: %w", err))
	}
	plain := keyPrefix + base64.RawURLEncoding.EncodeToString(secret)
	k := APIKey{
		ID:        ids.New(),
		TenantID:  tenantID,
		Name:      name,
		Prefix:    plain[:len(keyPrefix)+displayPrefixN],
		CreatedAt: now,
	}
	return k, plain, hashKey(plain), nil
}

// hashKey uses plain SHA-256: keys are 256 bits of randomness, so a slow KDF adds nothing.
func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}
