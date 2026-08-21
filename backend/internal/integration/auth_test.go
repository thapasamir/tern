//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/config"
)

const goodPassword = "correct horse battery"

// signup registers a new account with a unique email and returns the response.
func (s *stack) signup(email, tenantName string) response {
	s.t.Helper()
	return s.call(http.MethodPost, "/auth/signup", "", map[string]any{
		"email": email, "password": goodPassword, "tenant_name": tenantName,
	})
}

func uniqueEmail() string { return "user-" + uuid.NewString() + "@example.com" }

func TestSignedUpUserCanUseTheAPIWithTheirSessionToken(t *testing.T) {
	s := startStack(t, nil)
	rc := newReceiver(t)

	signup := s.signup(uniqueEmail(), "Session Co")
	if signup.Status != http.StatusCreated {
		t.Fatalf("signup: status %d, body %v", signup.Status, signup.Body)
	}
	token := signup.str("session", "token")
	if !strings.HasPrefix(token, "tsess_") {
		t.Fatalf("session token %q lacks the session prefix", token)
	}

	// The session reaches the same /v1 API an API key does, for the tenant signup created.
	tenant := s.call(http.MethodGet, "/v1/tenant", token, nil)
	if tenant.str("id") != signup.str("tenant", "id") || tenant.str("name") != "Session Co" {
		t.Fatalf("/v1/tenant = %v, want the tenant created by signup", tenant.Body)
	}
	epID, secret := s.newEndpoint(token, rc.srv.URL)
	rc.setSecret(secret)
	eventID := s.sendEvent(token, "from.dashboard")
	eventually(t, "the delivery to finish", func() bool { return s.settled(token, eventID, 1) })
	if d := s.deliveriesOf(token, eventID)[0].(map[string]any); d["status"] != "succeeded" || d["endpoint_id"] != epID {
		t.Fatalf("delivery = %v, want succeeded to %s", d, epID)
	}

	me := s.call(http.MethodGet, "/auth/me", token, nil)
	if me.Status != http.StatusOK || me.str("tenant", "id") != signup.str("tenant", "id") {
		t.Fatalf("/auth/me = %d %v", me.Status, me.Body)
	}
}

func TestAPIKeysCannotBeUsedAsSessions(t *testing.T) {
	s := startStack(t, nil)
	apiKey := s.newTenant("Key only")
	if got := s.call(http.MethodGet, "/auth/me", apiKey, nil).Status; got != http.StatusUnauthorized {
		t.Fatalf("/auth/me with an API key: status %d, want 401", got)
	}
}

func TestLoginAndLogout(t *testing.T) {
	s := startStack(t, nil)
	email := uniqueEmail()
	first := s.signup(email, "Login Co").str("session", "token")

	login := func(password string) response {
		return s.call(http.MethodPost, "/auth/login", "", map[string]any{"email": strings.ToUpper(email), "password": password})
	}
	if got := login("wrong password").Status; got != http.StatusUnauthorized {
		t.Fatalf("login with a wrong password: status %d, want 401", got)
	}
	unknown := s.call(http.MethodPost, "/auth/login", "", map[string]any{"email": uniqueEmail(), "password": goodPassword})
	wrong := login("wrong password")
	if unknown.Status != http.StatusUnauthorized || unknown.str("error", "message") != wrong.str("error", "message") {
		t.Fatalf("unknown email answered %d %q, wrong password %q; they must look the same",
			unknown.Status, unknown.str("error", "message"), wrong.str("error", "message"))
	}

	ok := login(goodPassword)
	if ok.Status != http.StatusOK {
		t.Fatalf("login: status %d, body %v", ok.Status, ok.Body)
	}
	second := ok.str("session", "token")
	if second == first {
		t.Fatal("logging in returned the signup session instead of a new one")
	}

	if got := s.call(http.MethodPost, "/auth/logout", second, nil).Status; got != http.StatusNoContent {
		t.Fatalf("logout: status %d, want 204", got)
	}
	if got := s.call(http.MethodGet, "/auth/me", second, nil).Status; got != http.StatusUnauthorized {
		t.Fatalf("the session still works after logout: status %d", got)
	}
	if got := s.call(http.MethodGet, "/v1/tenant", second, nil).Status; got != http.StatusUnauthorized {
		t.Fatalf("the session still reaches /v1 after logout: status %d", got)
	}
	if got := s.call(http.MethodGet, "/auth/me", first, nil).Status; got != http.StatusOK {
		t.Fatalf("logging out one session ended another: status %d", got)
	}
}

func TestSignupRefusesARegisteredEmailAndLeavesNoTenantBehind(t *testing.T) {
	s := startStack(t, nil)
	email := uniqueEmail()
	if got := s.signup(email, "First Co").Status; got != http.StatusCreated {
		t.Fatalf("first signup: status %d", got)
	}
	name := "Second Co " + uuid.NewString()
	if got := s.signup(strings.ToUpper(email), name).Status; got != http.StatusConflict {
		t.Fatalf("second signup with the same email: status %d, want 409", got)
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tenants WHERE name = $1`, name).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d tenants named %q exist (err %v); a refused signup must roll back", n, name, err)
	}
}

func TestSignupValidatesInput(t *testing.T) {
	s := startStack(t, nil)
	for name, body := range map[string]map[string]any{
		"no email":       {"password": goodPassword, "tenant_name": "X"},
		"bad email":      {"email": "nope", "password": goodPassword, "tenant_name": "X"},
		"short password": {"email": uniqueEmail(), "password": "short", "tenant_name": "X"},
		"no tenant name": {"email": uniqueEmail(), "password": goodPassword},
	} {
		if got := s.call(http.MethodPost, "/auth/signup", "", body).Status; got != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, got)
		}
	}
}

func TestPasswordsAndTokensAreNeverStoredInPlaintext(t *testing.T) {
	s := startStack(t, nil)
	email := uniqueEmail()
	token := s.signup(email, "Hashing Co").str("session", "token")

	ctx := context.Background()
	var hash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, goodPassword) {
		t.Fatalf("stored password hash %q is not an argon2id hash", hash)
	}
	var tokensLikeIt int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash = convert_to($1, 'UTF8')`, token).Scan(&tokensLikeIt); err != nil || tokensLikeIt != 0 {
		t.Fatalf("the session token is stored as it is (count %d, err %v)", tokensLikeIt, err)
	}
}

func TestExpiredSessionsAreRejected(t *testing.T) {
	s := startStack(t, nil)
	email := uniqueEmail()
	token := s.signup(email, "Expiry Co").str("session", "token")

	if _, err := pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE user_id = (SELECT id FROM users WHERE email = $1)`, email); err != nil {
		t.Fatal(err)
	}
	if got := s.call(http.MethodGet, "/v1/tenant", token, nil).Status; got != http.StatusUnauthorized {
		t.Fatalf("an expired session reached /v1: status %d, want 401", got)
	}
}

func TestSessionsOfOneTenantCannotSeeAnothers(t *testing.T) {
	s := startStack(t, nil)
	a := s.signup(uniqueEmail(), "Tenant A").str("session", "token")
	b := s.signup(uniqueEmail(), "Tenant B").str("session", "token")

	epID, _ := s.newEndpoint(a, "http://127.0.0.1:1/unused")
	if got := s.call(http.MethodGet, "/v1/endpoints/"+epID, b, nil).Status; got != http.StatusNotFound {
		t.Fatalf("tenant B reading tenant A's endpoint: status %d, want 404", got)
	}
}

func TestLoginAttemptsAreRateLimitedPerClient(t *testing.T) {
	s := startStack(t, nil, func(c *config.Config) { c.AuthAttemptsPerMinute = 3 })
	body := map[string]any{"email": uniqueEmail(), "password": "whatever you like"}

	var statuses []int
	for range 5 {
		r := s.call(http.MethodPost, "/auth/login", "", body)
		statuses = append(statuses, r.Status)
		if r.Status == http.StatusTooManyRequests && r.str("error", "code") == "" {
			t.Fatalf("the 429 has no error body: %v", r.Body)
		}
	}
	want := fmt.Sprint([]int{401, 401, 401, 429, 429})
	if fmt.Sprint(statuses) != want {
		t.Fatalf("statuses = %v, want %s (three attempts, then refused)", statuses, want)
	}
}
