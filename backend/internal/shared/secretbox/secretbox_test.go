package secretbox

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testKey(fill byte) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(fill), KeyBytes)))
}

func newBox(t *testing.T, fill byte) *Box {
	t.Helper()
	b, err := New(testKey(fill))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestSealOpenRoundTrip(t *testing.T) {
	b := newBox(t, 'a')
	const secret = "whsec_c2VjcmV0"

	sealed, err := b.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, secret) || !strings.HasPrefix(sealed, sealedPrefix) {
		t.Fatalf("sealed value %q leaks the secret or lacks the prefix", sealed)
	}
	got, err := b.Open(sealed)
	if err != nil || got != secret {
		t.Fatalf("Open = %q, %v; want %q", got, err, secret)
	}
}

func TestSealIsRandomised(t *testing.T) {
	b := newBox(t, 'a')
	first, _ := b.Seal("same")
	second, _ := b.Seal("same")
	if first == second {
		t.Fatal("sealing twice produced identical output; the nonce is not random")
	}
}

func TestOpenPassesLegacyPlaintextThrough(t *testing.T) {
	got, err := newBox(t, 'a').Open("whsec_legacy")
	if err != nil || got != "whsec_legacy" {
		t.Fatalf("Open(legacy) = %q, %v", got, err)
	}
}

func TestOpenRejectsWrongKeyAndDamage(t *testing.T) {
	sealed, _ := newBox(t, 'a').Seal("whsec_x")

	tests := map[string]string{
		"wrong key":   sealed, // opened below with a different box
		"not base64":  sealedPrefix + "!!!",
		"too short":   sealedPrefix + base64.StdEncoding.EncodeToString([]byte("tiny")),
		"tampered":    sealed[:len(sealed)-4] + "AAAA",
		"empty value": sealedPrefix,
	}
	other := newBox(t, 'b')
	for name, stored := range tests {
		if _, err := other.Open(stored); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: err = %v, want ErrDecrypt", name, err)
		}
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	for name, key := range map[string]string{
		"empty":      "",
		"not base64": "***",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		if _, err := New(key); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
