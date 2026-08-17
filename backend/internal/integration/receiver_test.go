//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/shared/signing"
)

// receiver is a fake customer endpoint. It verifies the signature of every request, so a test
// that sees a request here has also proven the signing secret survived storage and encryption.
type receiver struct {
	srv *httptest.Server

	mu         sync.Mutex
	secret     string
	badSignals int // requests whose signature did not verify

	hits   atomic.Int64
	status atomic.Int64 // the status code to answer with
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	rc := &receiver{}
	rc.status.Store(http.StatusOK)
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		if err := signing.Verify(rc.secret, r.Header, body, time.Now(), time.Minute); err != nil {
			rc.badSignals++
		}
		rc.mu.Unlock()
		rc.hits.Add(1)
		w.WriteHeader(int(rc.status.Load()))
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

// setSecret tells the receiver which secret to verify with (known only once the endpoint exists).
func (rc *receiver) setSecret(secret string) {
	rc.mu.Lock()
	rc.secret = secret
	rc.mu.Unlock()
}

func (rc *receiver) invalidSignatures() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.badSignals
}

// answerWith changes the status code the receiver replies with from now on.
func (rc *receiver) answerWith(code int) { rc.status.Store(int64(code)) }

func (rc *receiver) received() int { return int(rc.hits.Load()) }

// endpointFor registers an endpoint pointing at rc and tells rc its secret.
func (rc *receiver) endpointFor(s *stack, key string) (id string) {
	id, secret := s.newEndpoint(key, rc.srv.URL)
	rc.setSecret(secret)
	return id
}
