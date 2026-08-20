package user

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

var t0 = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

// fakeHasher is cheap and transparent: the "hash" is the password with a prefix.
type fakeHasher struct{ verified []string }

func (h *fakeHasher) Hash(p string) (string, error) { return "hash:" + p, nil }
func (h *fakeHasher) Verify(p, hash string) (bool, error) {
	h.verified = append(h.verified, hash)
	if !strings.HasPrefix(hash, "hash:") {
		return false, errors.New("unreadable hash")
	}
	return "hash:"+p == hash, nil
}

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	tenants  map[uuid.UUID]Tenant
	users    map[string]User // by email
	sessions map[string]storedSession
	pruned   []uuid.UUID
	pruneErr error
}

type storedSession struct {
	Session
	TenantID uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{tenants: map[uuid.UUID]Tenant{}, users: map[string]User{}, sessions: map[string]storedSession{}}
}

func (r *fakeRepo) CreateAccount(_ context.Context, t Tenant, u User, s Session, hash []byte) error {
	if _, taken := r.users[u.Email]; taken {
		return ErrEmailTaken
	}
	r.tenants[t.ID] = t
	r.users[u.Email] = u
	r.sessions[string(hash)] = storedSession{s, u.TenantID}
	return nil
}

func (r *fakeRepo) CreateSession(_ context.Context, s Session, hash []byte) error {
	for _, u := range r.users {
		if u.ID == s.UserID {
			r.sessions[string(hash)] = storedSession{s, u.TenantID}
			return nil
		}
	}
	return errors.New("no such user")
}

func (r *fakeRepo) FindUserByEmail(_ context.Context, email string) (User, error) {
	if u, ok := r.users[email]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (r *fakeRepo) GetAccount(_ context.Context, id uuid.UUID) (Account, error) {
	for _, u := range r.users {
		if u.ID == id {
			return Account{User: u, Tenant: r.tenants[u.TenantID]}, nil
		}
	}
	return Account{}, ErrNotFound
}

func (r *fakeRepo) FindSession(_ context.Context, hash []byte, now time.Time) (Principal, error) {
	s, ok := r.sessions[string(hash)]
	if !ok || !s.ExpiresAt.After(now) {
		return Principal{}, ErrNotFound
	}
	return Principal{UserID: s.UserID, TenantID: s.TenantID, SessionID: s.ID}, nil
}

func (r *fakeRepo) DeleteSession(_ context.Context, id uuid.UUID) error {
	for k, s := range r.sessions {
		if s.ID == id {
			delete(r.sessions, k)
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeRepo) DeleteExpiredSessions(_ context.Context, userID uuid.UUID, _ time.Time) error {
	r.pruned = append(r.pruned, userID)
	return r.pruneErr
}

type fixture struct {
	svc    *Service
	repo   *fakeRepo
	hasher *fakeHasher
	now    time.Time
}

func newFixture() *fixture {
	f := &fixture{repo: newFakeRepo(), hasher: &fakeHasher{}, now: t0}
	f.svc = NewService(f.repo, f.hasher)
	f.svc.now = func() time.Time { return f.now }
	f.hasher.verified = nil // forget the decoy hash made by NewService
	return f
}

func (f *fixture) signup(t *testing.T) Login {
	t.Helper()
	l, err := f.svc.Signup(context.Background(), SignupInput{Email: "Ada@Example.com ", Password: "correct horse", TenantName: " Analytical Engines "})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	return l
}

func TestSignup_CreatesTenantUserAndSession(t *testing.T) {
	f := newFixture()
	l := f.signup(t)

	if l.Account.User.Email != "ada@example.com" || l.Account.Tenant.Name != "Analytical Engines" {
		t.Fatalf("account = %+v, want the email lower-cased and the name trimmed", l.Account)
	}
	if l.Account.User.TenantID != l.Account.Tenant.ID {
		t.Fatal("the user does not belong to the tenant signup created")
	}
	if !strings.HasPrefix(l.Token, sessionPrefix) || !l.ExpiresAt.Equal(t0.Add(SessionTTL)) {
		t.Fatalf("token %q expires %v, want the session prefix and %v", l.Token, l.ExpiresAt, t0.Add(SessionTTL))
	}
	stored := f.repo.users["ada@example.com"]
	if stored.PasswordHash != "hash:correct horse" || strings.Contains(stored.PasswordHash, "Ada") {
		t.Fatalf("stored password hash = %q, want the hasher's output", stored.PasswordHash)
	}
	if _, plainStored := f.repo.sessions[l.Token]; plainStored {
		t.Fatal("the session token was stored in plaintext; only its hash may be")
	}
}

func TestSignup_RejectsBadInput(t *testing.T) {
	tests := map[string]SignupInput{
		"empty email":          {Email: "", Password: "correct horse", TenantName: "Acme"},
		"not an address":       {Email: "ada", Password: "correct horse", TenantName: "Acme"},
		"display name":         {Email: "Ada <ada@example.com>", Password: "correct horse", TenantName: "Acme"},
		"no dot in the host":   {Email: "ada@localhost", Password: "correct horse", TenantName: "Acme"},
		"too long an address":  {Email: strings.Repeat("a", 250) + "@example.com", Password: "correct horse", TenantName: "Acme"},
		"short password":       {Email: "ada@example.com", Password: "short", TenantName: "Acme"},
		"huge password":        {Email: "ada@example.com", Password: strings.Repeat("p", maxPasswordLen+1), TenantName: "Acme"},
		"no tenant name":       {Email: "ada@example.com", Password: "correct horse", TenantName: "  "},
		"tenant name too long": {Email: "ada@example.com", Password: "correct horse", TenantName: strings.Repeat("n", maxNameLen+1)},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, err := f.svc.Signup(context.Background(), in)
			testutil.WantCode(t, err, apperror.CodeInvalid)
			if len(f.repo.users) != 0 {
				t.Fatal("a rejected signup stored something")
			}
		})
	}
}

func TestSignup_EmailIsUniqueIgnoringCase(t *testing.T) {
	f := newFixture()
	f.signup(t)
	_, err := f.svc.Signup(context.Background(), SignupInput{Email: "ADA@example.com", Password: "another pass", TenantName: "Other"})
	testutil.WantCode(t, err, apperror.CodeConflict)
	if len(f.repo.tenants) != 1 {
		t.Fatalf("%d tenants stored, want 1: a refused signup must not leave a tenant behind", len(f.repo.tenants))
	}
}

func TestLogin(t *testing.T) {
	f := newFixture()
	f.signup(t)

	l, err := f.svc.Login(context.Background(), " ADA@example.com", "correct horse")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if l.Account.Tenant.Name != "Analytical Engines" || !strings.HasPrefix(l.Token, sessionPrefix) {
		t.Fatalf("login = %+v", l)
	}
	if len(f.repo.sessions) != 2 {
		t.Fatalf("%d sessions, want 2 (signup's and this login's): logging in must not end other sessions", len(f.repo.sessions))
	}
	if len(f.repo.pruned) != 1 {
		t.Fatal("expired sessions of the user were not cleaned up")
	}
}

func TestLogin_FailureIsTheSameWhateverTheReason(t *testing.T) {
	f := newFixture()
	f.signup(t)

	attempts := [][2]string{
		{"ada@example.com", "wrong horse"},      // wrong password
		{"nobody@example.com", "correct horse"}, // unknown email
		{"not an email", "correct horse"},       // malformed email
	}
	var messages []string
	for _, a := range attempts {
		_, err := f.svc.Login(context.Background(), a[0], a[1])
		testutil.WantCode(t, err, apperror.CodeUnauthorized)
		messages = append(messages, apperror.From(err).Message)
	}
	for _, m := range messages[1:] {
		if m != messages[0] {
			t.Fatalf("failure messages differ (%q vs %q): they would reveal which emails exist", m, messages[0])
		}
	}
}

func TestLogin_UnknownEmailStillCostsAHashCheck(t *testing.T) {
	f := newFixture()
	_, _ = f.svc.Login(context.Background(), "nobody@example.com", "whatever")
	if len(f.hasher.verified) != 1 || f.hasher.verified[0] != f.svc.decoyHash {
		t.Fatalf("verified %v, want exactly one check against the decoy hash, so the timing matches a real login", f.hasher.verified)
	}
}

func TestLogin_AnUnreadableStoredHashIsAServerProblem(t *testing.T) {
	f := newFixture()
	f.signup(t)
	u := f.repo.users["ada@example.com"]
	u.PasswordHash = "garbage"
	f.repo.users["ada@example.com"] = u

	_, err := f.svc.Login(context.Background(), "ada@example.com", "correct horse")
	testutil.WantCode(t, err, apperror.CodeInternal)
}

func TestLogin_AFailedCleanupDoesNotFailTheLogin(t *testing.T) {
	f := newFixture()
	f.signup(t)
	f.repo.pruneErr = errors.New("database hiccup")
	if _, err := f.svc.Login(context.Background(), "ada@example.com", "correct horse"); err != nil {
		t.Fatalf("Login failed because housekeeping failed: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	f := newFixture()
	l := f.signup(t)

	p, err := f.svc.Authenticate(context.Background(), l.Token)
	if err != nil || p.UserID != l.Account.User.ID || p.TenantID != l.Account.Tenant.ID {
		t.Fatalf("Authenticate = %+v, %v; want the signed-up user and tenant", p, err)
	}

	for _, token := range []string{"", "tern_abcdefghijklmnopqrstuvwxyz", sessionPrefix + "doesnotexist"} {
		_, err := f.svc.Authenticate(context.Background(), token)
		testutil.WantCode(t, err, apperror.CodeUnauthorized)
	}

	f.now = t0.Add(SessionTTL)
	_, err = f.svc.Authenticate(context.Background(), l.Token)
	testutil.WantCode(t, err, apperror.CodeUnauthorized)
}

func TestLogout_EndsOnlyThatSession(t *testing.T) {
	f := newFixture()
	first := f.signup(t)
	second, err := f.svc.Login(context.Background(), "ada@example.com", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := f.svc.Authenticate(context.Background(), first.Token)

	if err := f.svc.Logout(context.Background(), p); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), first.Token); err == nil {
		t.Fatal("the session still works after logout")
	}
	if _, err := f.svc.Authenticate(context.Background(), second.Token); err != nil {
		t.Fatalf("logging out one session ended another: %v", err)
	}
	if err := f.svc.Logout(context.Background(), p); err != nil {
		t.Fatalf("logging out twice must not be an error: %v", err)
	}
}

func TestMe(t *testing.T) {
	f := newFixture()
	l := f.signup(t)
	p, _ := f.svc.Authenticate(context.Background(), l.Token)

	a, err := f.svc.Me(context.Background(), p)
	if err != nil || a.User.Email != "ada@example.com" || a.Tenant.Name != "Analytical Engines" {
		t.Fatalf("Me = %+v, %v", a, err)
	}
}
