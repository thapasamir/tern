package signing

import (
	"encoding/base64"
	"strings"
	"testing"
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
