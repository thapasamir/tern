package endpoint

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/webhook-platform/internal/shared/apperror"
	"github.com/samirthapa/webhook-platform/internal/shared/ids"
	"github.com/samirthapa/webhook-platform/internal/shared/pagination"
	"github.com/samirthapa/webhook-platform/internal/shared/signing"
)

const (
	maxDescriptionLen = 500
	maxEventTypes     = 50
	maxEventTypeLen   = 128
)

// eventTypePattern accepts dotted lowercase names such as "invoice.paid" or "user.created".
var eventTypePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// URLValidator decides whether a webhook URL is acceptable.
// Its error messages must be safe to show to API clients.
type URLValidator interface {
	Validate(raw string) error
}

// CreateInput holds the customer-supplied fields of a new endpoint.
type CreateInput struct {
	URL         string
	Description string
	EventTypes  []string
}

// UpdateInput is a partial update: nil fields are left unchanged.
type UpdateInput struct {
	URL         *string
	Description *string
	EventTypes  *[]string
}

// Service holds the business rules for endpoints.
type Service struct {
	repo Repository
	urls URLValidator
	now  func() time.Time
}

func NewService(repo Repository, urls URLValidator) *Service {
	return &Service{repo: repo, urls: urls, now: time.Now}
}

// Create registers a new endpoint with a freshly generated signing secret.
func (s *Service) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (Endpoint, error) {
	url, err := s.validateURL(in.URL)
	if err != nil {
		return Endpoint{}, err
	}
	description, err := validateDescription(in.Description)
	if err != nil {
		return Endpoint{}, err
	}
	eventTypes, err := normalizeEventTypes(in.EventTypes)
	if err != nil {
		return Endpoint{}, err
	}
	secret, err := signing.NewSecret()
	if err != nil {
		return Endpoint{}, apperror.Internal(err)
	}

	now := s.now().UTC()
	e := Endpoint{
		ID:          ids.New(),
		TenantID:    tenantID,
		URL:         url,
		Description: description,
		EventTypes:  eventTypes,
		Secret:      secret,
		Status:      StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, e); err != nil {
		return Endpoint{}, apperror.Internal(err)
	}
	return e, nil
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (Endpoint, error) {
	e, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return Endpoint{}, mapRepoError(err)
	}
	return e, nil
}

func (s *Service) List(ctx context.Context, tenantID uuid.UUID, p pagination.Params) (pagination.Result[Endpoint], error) {
	rows, err := s.repo.List(ctx, tenantID, p)
	if err != nil {
		return pagination.Result[Endpoint]{}, apperror.Internal(err)
	}
	return pagination.NewResult(rows, p, func(e Endpoint) uuid.UUID { return e.ID }), nil
}

// Update applies a partial update. Only the fields that are provided are validated, so an
// endpoint stored under older rules can still have, say, its description edited.
func (s *Service) Update(ctx context.Context, tenantID, id uuid.UUID, in UpdateInput) (Endpoint, error) {
	e, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return Endpoint{}, err
	}

	if in.URL != nil {
		if e.URL, err = s.validateURL(*in.URL); err != nil {
			return Endpoint{}, err
		}
	}
	if in.Description != nil {
		if e.Description, err = validateDescription(*in.Description); err != nil {
			return Endpoint{}, err
		}
	}
	if in.EventTypes != nil {
		if e.EventTypes, err = normalizeEventTypes(*in.EventTypes); err != nil {
			return Endpoint{}, err
		}
	}

	e.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, e); err != nil {
		return Endpoint{}, mapRepoError(err)
	}
	return e, nil
}

// RotateSecret replaces the signing secret and returns the endpoint with the new one.
// The old secret stops working immediately.
func (s *Service) RotateSecret(ctx context.Context, tenantID, id uuid.UUID) (Endpoint, error) {
	e, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return Endpoint{}, err
	}
	secret, err := signing.NewSecret()
	if err != nil {
		return Endpoint{}, apperror.Internal(err)
	}

	now := s.now().UTC()
	if err := s.repo.UpdateSecret(ctx, tenantID, id, secret, now); err != nil {
		return Endpoint{}, mapRepoError(err)
	}
	e.Secret, e.UpdatedAt = secret, now
	return e, nil
}

// Disable stops deliveries to the endpoint and records why. It is used both for manual
// disabling (DisabledReasonManual) and, later, for automatic disabling after repeated failures.
func (s *Service) Disable(ctx context.Context, tenantID, id uuid.UUID, reason string) (Endpoint, error) {
	if strings.TrimSpace(reason) == "" {
		return Endpoint{}, apperror.Internal(errors.New("endpoint: disable called without a reason"))
	}
	if err := s.repo.Disable(ctx, tenantID, id, reason, s.now().UTC()); err != nil {
		return Endpoint{}, mapRepoError(err)
	}
	return s.Get(ctx, tenantID, id)
}

// Enable resumes deliveries and clears the disabled reason.
func (s *Service) Enable(ctx context.Context, tenantID, id uuid.UUID) (Endpoint, error) {
	if err := s.repo.Enable(ctx, tenantID, id, s.now().UTC()); err != nil {
		return Endpoint{}, mapRepoError(err)
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Service) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, tenantID, id); err != nil {
		return mapRepoError(err)
	}
	return nil
}

func (s *Service) validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := s.urls.Validate(raw); err != nil {
		return "", apperror.Invalid(err.Error())
	}
	return raw, nil
}

func validateDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > maxDescriptionLen {
		return "", apperror.Invalid(fmt.Sprintf("description must be at most %d characters", maxDescriptionLen))
	}
	return d, nil
}

// normalizeEventTypes validates, trims and de-duplicates event types, keeping first-seen order.
// The result is never nil: an empty list means "all event types".
func normalizeEventTypes(in []string) ([]string, error) {
	if len(in) > maxEventTypes {
		return nil, apperror.Invalid(fmt.Sprintf("at most %d event types are allowed", maxEventTypes))
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if len(t) > maxEventTypeLen || !eventTypePattern.MatchString(t) {
			return nil, apperror.Invalid(fmt.Sprintf(
				"invalid event type %q: use lowercase letters, digits and . _ - (for example \"invoice.paid\")", t))
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out, nil
}

func mapRepoError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperror.NotFound("endpoint not found")
	}
	return apperror.Internal(err)
}
