package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ids"
	"github.com/samirthapa/tern/internal/shared/validate"
)

const (
	// sessionPrefix tells a session token from an API key (which starts with "tern_") at a glance,
	// in logs and secret scanners as well as in the authentication middleware.
	sessionPrefix = "tsess_"
	// SessionTTL is how long a login lasts. It is fixed from the moment of login: signing in again
	// is how a session is extended.
	SessionTTL = 30 * 24 * time.Hour

	tokenBytes     = 32
	maxEmailLen    = 254 // the limit in RFC 5321
	minPasswordLen = 8
	maxPasswordLen = 128 // argon2 is deliberately slow, so unbounded input would be a cheap way to burn CPU
	maxNameLen     = 100
)

// Hasher turns passwords into hashes and checks them. *password.Hasher implements it.
type Hasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) (bool, error)
}

type Service struct {
	repo   Repository
	hasher Hasher
	now    func() time.Time
	// decoyHash is checked when a login names an unknown email, so that such a login takes as
	// long as one with a wrong password and the response time does not reveal which emails exist.
	decoyHash string
}

func NewService(repo Repository, hasher Hasher) *Service {
	decoy, err := hasher.Hash("a password nobody has")
	if err != nil {
		// Only possible if the system's random source is broken: nothing can run safely then.
		panic(fmt.Sprintf("user: cannot create the decoy hash: %v", err))
	}
	return &Service{repo: repo, hasher: hasher, now: time.Now, decoyHash: decoy}
}

// SignupInput is what a new customer provides.
type SignupInput struct {
	Email      string
	Password   string
	TenantName string
}

// Signup creates a tenant, its first user and a session, and signs the user in. There is no email
// verification yet: anyone can register any address, and a taken address is reported as such.
func (s *Service) Signup(ctx context.Context, in SignupInput) (Login, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return Login{}, err
	}
	if err := validatePassword(in.Password); err != nil {
		return Login{}, err
	}
	name, err := validate.Text("tenant_name", in.TenantName, maxNameLen, true)
	if err != nil {
		return Login{}, err
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return Login{}, apperror.Internal(err)
	}

	now := s.now().UTC()
	tenant := Tenant{ID: ids.New(), Name: name, CreatedAt: now}
	user := User{ID: ids.New(), TenantID: tenant.ID, Email: email, PasswordHash: hash, CreatedAt: now}
	session, token, tokenHash, err := newSession(user.ID, now)
	if err != nil {
		return Login{}, err
	}

	if err := s.repo.CreateAccount(ctx, tenant, user, session, tokenHash); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return Login{}, apperror.Conflict("an account with this email already exists")
		}
		return Login{}, apperror.Internal(err)
	}
	return Login{Account: Account{User: user, Tenant: tenant}, Token: token, ExpiresAt: session.ExpiresAt}, nil
}

// Login checks the credentials and starts a session. A wrong password and an unknown email give
// the same answer, in the same time.
func (s *Service) Login(ctx context.Context, emailInput, password string) (Login, error) {
	invalid := apperror.Unauthorized("invalid email or password")

	email, err := normalizeEmail(emailInput)
	if err != nil {
		return Login{}, invalid // not even an address: it cannot be registered
	}
	user, err := s.repo.FindUserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		_, _ = s.hasher.Verify(password, s.decoyHash)
		return Login{}, invalid
	}
	if err != nil {
		return Login{}, apperror.Internal(err)
	}
	ok, err := s.hasher.Verify(password, user.PasswordHash)
	if err != nil {
		return Login{}, apperror.Internal(fmt.Errorf("verify password of user %s: %w", user.ID, err))
	}
	if !ok {
		return Login{}, invalid
	}

	now := s.now().UTC()
	session, token, tokenHash, err := newSession(user.ID, now)
	if err != nil {
		return Login{}, err
	}
	if err := s.repo.CreateSession(ctx, session, tokenHash); err != nil {
		return Login{}, apperror.Internal(err)
	}
	// Housekeeping, not part of the login: a failure here must not fail it.
	if err := s.repo.DeleteExpiredSessions(ctx, user.ID, now); err != nil {
		slog.Warn("deleting expired sessions failed", "user_id", user.ID, "error", err)
	}

	account, err := s.repo.GetAccount(ctx, user.ID)
	if err != nil {
		return Login{}, apperror.Internal(err)
	}
	return Login{Account: account, Token: token, ExpiresAt: session.ExpiresAt}, nil
}

// Logout ends the session. Ending one that is already gone is not an error.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	if err := s.repo.DeleteSession(ctx, p.SessionID); err != nil && !errors.Is(err, ErrNotFound) {
		return apperror.Internal(err)
	}
	return nil
}

// Me returns the account behind a session.
func (s *Service) Me(ctx context.Context, p Principal) (Account, error) {
	a, err := s.repo.GetAccount(ctx, p.UserID)
	if err != nil {
		return Account{}, apperror.FromStore(err, "account not found")
	}
	return a, nil
}

// Authenticate resolves a session token to its owner. Every failure gives the same answer.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	invalid := apperror.Unauthorized("invalid or expired session")
	if !strings.HasPrefix(token, sessionPrefix) {
		return Principal{}, invalid
	}
	p, err := s.repo.FindSession(ctx, hashToken(token), s.now().UTC())
	if errors.Is(err, ErrNotFound) {
		return Principal{}, invalid
	}
	if err != nil {
		return Principal{}, apperror.Internal(err)
	}
	return p, nil
}

// HasSessionPrefix reports whether token looks like a session token rather than an API key.
func HasSessionPrefix(token string) bool { return strings.HasPrefix(token, sessionPrefix) }

func newSession(userID uuid.UUID, now time.Time) (Session, string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Session{}, "", nil, apperror.Internal(fmt.Errorf("generate session token: %w", err))
	}
	token := sessionPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return Session{ID: ids.New(), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(SessionTTL)}, token, hashToken(token), nil
}

// hashToken uses plain SHA-256: tokens are 256 bits of randomness, so a slow hash adds nothing.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// normalizeEmail trims and lower-cases an address and checks that it is a single plain address.
func normalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", apperror.Invalid("email is required")
	}
	if len(s) > maxEmailLen {
		return "", apperror.Invalid(fmt.Sprintf("email must be at most %d characters", maxEmailLen))
	}
	// ParseAddress also accepts "Name <a@b.c>"; requiring the parsed address to equal the input
	// keeps display names and comments out of the login name.
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", apperror.Invalid("email is not a valid address")
	}
	return s, nil
}

func validatePassword(p string) error {
	switch n := utf8.RuneCountInString(p); {
	case n < minPasswordLen:
		return apperror.Invalid(fmt.Sprintf("password must be at least %d characters", minPasswordLen))
	case n > maxPasswordLen:
		return apperror.Invalid(fmt.Sprintf("password must be at most %d characters", maxPasswordLen))
	}
	return nil
}
