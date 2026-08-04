package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewSecret(t *testing.T) {
	s, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, SecretPrefix) {
		t.Fatalf("secret %q missing prefix %q", s, SecretPrefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, SecretPrefix))
	if err != nil {
		t.Fatalf("secret body is not valid base64: %v", err)
	}
	if len(raw) != secretBytes {
		t.Fatalf("decoded secret is %d bytes, want %d", len(raw), secretBytes)
	}
}

func TestNewSecret_IsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s, err := NewSecret()
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatalf("duplicate secret generated: %s", s)
		}
		seen[s] = true
	}
}

var (
	testSecret = SecretPrefix + base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	testNow    = time.Unix(1_700_000_000, 0)
	testBody   = []byte(`{"id":"evt_1","type":"invoice.paid","data":{"amount":42}}`)
)

func TestSign_MatchesAnIndependentComputation(t *testing.T) {
	// Recompute the signature by hand so the test pins down the wire format, not just round trips.
	mac := hmac.New(sha256.New, []byte("0123456789abcdef0123456789abcdef"))
	mac.Write([]byte("msg_1.1700000000." + string(testBody)))
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	got, err := Sign(testSecret, "msg_1", testNow, testBody)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

func TestHeaders(t *testing.T) {
	h, err := Headers(testSecret, "msg_1", testNow, testBody)
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("webhook-id") != "msg_1" || h.Get("webhook-timestamp") != "1700000000" ||
		!strings.HasPrefix(h.Get("webhook-signature"), "v1,") {
		t.Fatalf("unexpected headers: %v", h)
	}
}

func TestVerify_AcceptsAValidDelivery(t *testing.T) {
	h, _ := Headers(testSecret, "msg_1", testNow, testBody)
	if err := Verify(testSecret, h, testBody, testNow.Add(time.Minute), 5*time.Minute); err != nil {
		t.Fatalf("Verify = %v, want nil", err)
	}
}

func TestVerify_Rejections(t *testing.T) {
	valid := func() http.Header {
		h, _ := Headers(testSecret, "msg_1", testNow, testBody)
		return h
	}
	otherSecret := SecretPrefix + base64.StdEncoding.EncodeToString([]byte("a-completely-different-secret!!!"))

	cases := map[string]struct {
		secret string
		header func() http.Header
		body   []byte
		now    time.Time
		want   error
	}{
		"tampered body":      {testSecret, valid, []byte(`{"amount":9999}`), testNow, ErrNoMatchingSignature},
		"wrong secret":       {otherSecret, valid, testBody, testNow, ErrNoMatchingSignature},
		"tampered id":        {testSecret, func() http.Header { h := valid(); h.Set(HeaderID, "msg_2"); return h }, testBody, testNow, ErrNoMatchingSignature},
		"tampered timestamp": {testSecret, func() http.Header { h := valid(); h.Set(HeaderTimestamp, "1700000001"); return h }, testBody, testNow, ErrNoMatchingSignature},
		"too old":            {testSecret, valid, testBody, testNow.Add(6 * time.Minute), ErrTimestampOutOfRange},
		"from the future":    {testSecret, valid, testBody, testNow.Add(-6 * time.Minute), ErrTimestampOutOfRange},
		"missing signature":  {testSecret, func() http.Header { h := valid(); h.Del(HeaderSignature); return h }, testBody, testNow, ErrMissingHeaders},
		"missing id":         {testSecret, func() http.Header { h := valid(); h.Del(HeaderID); return h }, testBody, testNow, ErrMissingHeaders},
		"garbage timestamp":  {testSecret, func() http.Header { h := valid(); h.Set(HeaderTimestamp, "yesterday"); return h }, testBody, testNow, ErrMissingHeaders},
		"unknown version": {testSecret, func() http.Header {
			h := valid()
			h.Set(HeaderSignature, strings.Replace(h.Get(HeaderSignature), "v1,", "v2,", 1))
			return h
		}, testBody, testNow, ErrNoMatchingSignature},
		"not base64": {testSecret, func() http.Header { h := valid(); h.Set(HeaderSignature, "v1,!!!"); return h }, testBody, testNow, ErrNoMatchingSignature},
		"bad secret": {"nope", valid, testBody, testNow, ErrInvalidSecret},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := Verify(c.secret, c.header(), c.body, c.now, 5*time.Minute)
			if !errors.Is(err, c.want) {
				t.Fatalf("Verify = %v, want %v", err, c.want)
			}
		})
	}
}

func TestVerify_AcceptsAnyOfSeveralSignatures(t *testing.T) {
	// During secret rotation a delivery can carry a signature per secret.
	h, _ := Headers(testSecret, "msg_1", testNow, testBody)
	h.Set(HeaderSignature, "v1,AAAA "+h.Get(HeaderSignature))
	if err := Verify(testSecret, h, testBody, testNow, time.Minute); err != nil {
		t.Fatalf("Verify with multiple signatures = %v, want nil", err)
	}
}

func TestSign_RejectsMalformedSecrets(t *testing.T) {
	for _, s := range []string{"", "plain", SecretPrefix, SecretPrefix + "not base64 !!"} {
		if _, err := Sign(s, "m", testNow, nil); !errors.Is(err, ErrInvalidSecret) {
			t.Errorf("Sign(%q) = %v, want ErrInvalidSecret", s, err)
		}
	}
}
