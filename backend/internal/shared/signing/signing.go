// Package signing creates webhook signing secrets and signs and verifies webhook requests.
//
// The scheme follows the Standard Webhooks specification so customers can verify deliveries with
// off-the-shelf libraries. A delivery carries three headers:
//
//	webhook-id         the event id, stable across retries (use it to de-duplicate)
//	webhook-timestamp  unix seconds when this attempt was signed
//	webhook-signature  "v1,<base64 HMAC-SHA256>" of "<id>.<timestamp>.<body>"
//
// The HMAC key is the secret's bytes: the part after "whsec_", base64-decoded.
package signing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// SecretPrefix makes secrets recognisable in logs and secret scanners.
	SecretPrefix = "whsec_"
	secretBytes  = 32

	HeaderID        = "Webhook-Id"
	HeaderTimestamp = "Webhook-Timestamp"
	HeaderSignature = "Webhook-Signature"

	signatureVersion = "v1"
)

var (
	ErrInvalidSecret       = errors.New("signing: invalid secret")
	ErrMissingHeaders      = errors.New("signing: missing webhook headers")
	ErrTimestampOutOfRange = errors.New("signing: timestamp outside the tolerated window")
	ErrNoMatchingSignature = errors.New("signing: no matching signature")
)

// NewSecret returns a new random signing secret: SecretPrefix + base64(32 random bytes).
func NewSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signing: generate secret: %w", err)
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(b), nil
}

// Sign returns the "v1,<base64>" signature for one delivery attempt.
func Sign(secret, msgID string, ts time.Time, body []byte) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	return signatureVersion + "," + base64.StdEncoding.EncodeToString(mac(key, msgID, ts.Unix(), body)), nil
}

// Headers returns the three webhook headers for one delivery attempt.
func Headers(secret, msgID string, ts time.Time, body []byte) (http.Header, error) {
	sig, err := Sign(secret, msgID, ts, body)
	if err != nil {
		return nil, err
	}
	h := make(http.Header, 3)
	h.Set(HeaderID, msgID)
	h.Set(HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
	h.Set(HeaderSignature, sig)
	return h, nil
}

// Verify checks a received request the way a customer should: the timestamp must be within
// tolerance of now (replay protection) and one of the signatures must match.
// The signature header may hold several space-separated signatures, e.g. during secret rotation.
func Verify(secret string, h http.Header, body []byte, now time.Time, tolerance time.Duration) error {
	key, err := decodeSecret(secret)
	if err != nil {
		return err
	}
	msgID, tsHeader, sigHeader := h.Get(HeaderID), h.Get(HeaderTimestamp), h.Get(HeaderSignature)
	if msgID == "" || tsHeader == "" || sigHeader == "" {
		return ErrMissingHeaders
	}
	unix, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return ErrMissingHeaders
	}
	if d := now.Sub(time.Unix(unix, 0)); d > tolerance || d < -tolerance {
		return ErrTimestampOutOfRange
	}

	expected := mac(key, msgID, unix, body)
	for _, candidate := range strings.Fields(sigHeader) {
		version, encoded, ok := strings.Cut(candidate, ",")
		if !ok || version != signatureVersion {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) { // constant time
			return nil
		}
	}
	return ErrNoMatchingSignature
}

func mac(key []byte, msgID string, unix int64, body []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msgID))
	h.Write([]byte("."))
	h.Write([]byte(strconv.FormatInt(unix, 10)))
	h.Write([]byte("."))
	h.Write(body)
	return h.Sum(nil)
}

func decodeSecret(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, SecretPrefix)
	if !ok {
		return nil, ErrInvalidSecret
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) == 0 {
		return nil, ErrInvalidSecret
	}
	return key, nil
}
