// Command receiver is a fake customer webhook endpoint for development and tests.
//
// It verifies webhook signatures exactly as a customer should, records what it received, and
// can be told to misbehave through the URL path:
//
//	/ok (or any other path)   200
//	/fail                     500
//	/status/{code}            responds with that status code
//	/slow?ms=N                waits N milliseconds (default 30000), then 200
//
// GET /_received lists the recorded requests; DELETE /_received clears them.
//
// Register an endpoint such as http://localhost:9099/fail with the API (needs ALLOW_LOCAL_ENDPOINTS=true).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samirthapa/webhook-platform/internal/shared/signing"
)

const (
	maxBodyBytes   = 1 << 20
	maxRecorded    = 200
	tolerance      = 5 * time.Minute
	defaultSlowMs  = 30_000
	receivedPath   = "/_received"
	readHeaderWait = 5 * time.Second
)

// record is one request the receiver saw.
type record struct {
	ReceivedAt  time.Time       `json:"received_at"`
	Path        string          `json:"path"`
	WebhookID   string          `json:"webhook_id"`
	Verified    bool            `json:"verified"`
	VerifyError string          `json:"verify_error,omitempty"`
	Body        json.RawMessage `json:"body"`
}

type receiver struct {
	secrets []string // empty disables signature checking; a request is valid if any secret verifies it

	mu       sync.Mutex
	received []record
}

func main() {
	addr := flag.String("addr", ":9099", "address to listen on")
	secret := flag.String("secret", os.Getenv("RECEIVER_SECRET"), "endpoint signing secret(s) (whsec_...), comma-separated; empty disables verification")
	flag.Parse()

	rcv := &receiver{secrets: splitSecrets(*secret)}
	srv := &http.Server{Addr: *addr, Handler: rcv, ReadHeaderTimeout: readHeaderWait}

	mode := "signature verification OFF"
	if len(rcv.secrets) > 0 {
		mode = fmt.Sprintf("verifying signatures against %d secret(s)", len(rcv.secrets))
	}
	log.Printf("receiver listening on %s (%s)", *addr, mode)
	log.Fatal(srv.ListenAndServe())
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == receivedPath {
		rc.serveRecords(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	rec := record{
		ReceivedAt: time.Now().UTC(),
		Path:       r.URL.Path,
		WebhookID:  r.Header.Get(signing.HeaderID),
		Body:       json.RawMessage(body),
	}
	if !json.Valid(body) {
		rec.Body = nil
	}

	if len(rc.secrets) > 0 {
		if err := rc.verify(r.Header, body); err != nil {
			rec.VerifyError = err.Error()
			rc.add(rec)
			log.Printf("%s  REJECTED  %v", r.URL.Path, err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		rec.Verified = true
	}
	rc.add(rec)

	status, ok := behaviour(r)
	if !ok {
		return // the client went away while we were being slow
	}
	log.Printf("%s  id=%s verified=%v -> %d", r.URL.Path, rec.WebhookID, rec.Verified, status)
	w.WriteHeader(status)
	fmt.Fprintf(w, "status %d\n", status)
}

// behaviour decides the response from the URL path. ok is false if the client disconnected
// while a slow response was pending.
func behaviour(r *http.Request) (status int, ok bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch parts[0] {
	case "fail":
		return http.StatusInternalServerError, true
	case "status":
		if len(parts) > 1 {
			if code, err := strconv.Atoi(parts[1]); err == nil && code >= 100 && code <= 599 {
				return code, true
			}
		}
		return http.StatusBadRequest, true
	case "slow":
		ms := defaultSlowMs
		if v, err := strconv.Atoi(r.URL.Query().Get("ms")); err == nil && v >= 0 {
			ms = v
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return http.StatusOK, true
		case <-r.Context().Done():
			return 0, false
		}
	default:
		return http.StatusOK, true
	}
}

// verify accepts a request signed with any configured secret, as a customer would during rotation.
func (rc *receiver) verify(h http.Header, body []byte) (err error) {
	for _, secret := range rc.secrets {
		if err = signing.Verify(secret, h, body, time.Now(), tolerance); err == nil {
			return nil
		}
	}
	return err
}

func splitSecrets(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (rc *receiver) add(rec record) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.received = append(rc.received, rec)
	if len(rc.received) > maxRecorded {
		rc.received = rc.received[len(rc.received)-maxRecorded:]
	}
}

func (rc *receiver) serveRecords(w http.ResponseWriter, r *http.Request) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		out := rc.received
		if out == nil {
			out = []record{} // JSON [] rather than null
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodDelete:
		rc.received = nil
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
