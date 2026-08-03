package urlguard

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestValidate_Production(t *testing.T) {
	v := NewValidator(false)

	valid := []string{
		"https://example.com/hook",
		"https://hooks.example.com:8443/a/b?x=1",
		"https://8.8.8.8/hook",
		"https://[2606:4700::1111]/hook",
	}
	for _, raw := range valid {
		t.Run("valid "+raw, func(t *testing.T) {
			if err := v.Validate(raw); err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", raw, err)
			}
		})
	}

	invalid := map[string]string{
		"":                                    "required",
		"not a url":                           "https",
		"ftp://example.com/hook":              "https",
		"http://example.com/hook":             "https",
		"https://":                            "host",
		"https:///path":                       "host",
		"https://user:pass@example.com/":      "credentials",
		"https://localhost/hook":              "not allowed",
		"https://LOCALHOST./hook":             "not allowed",
		"https://api.localhost/hook":          "not allowed",
		"https://printer.local/hook":          "not allowed",
		"https://db.internal/hook":            "not allowed",
		"https://127.0.0.1/hook":              "not allowed",
		"https://10.0.0.5/hook":               "not allowed",
		"https://172.16.0.1/hook":             "not allowed",
		"https://192.168.1.1/hook":            "not allowed",
		"https://169.254.169.254/latest/meta": "not allowed",
		"https://100.64.0.1/hook":             "not allowed",
		"https://0.0.0.0/hook":                "not allowed",
		"https://255.255.255.255/hook":        "not allowed",
		"https://[::1]/hook":                  "not allowed",
		"https://[fd00::1]/hook":              "not allowed",
		"https://[fe80::1]/hook":              "not allowed",
		"https://[::ffff:127.0.0.1]/hook":     "not allowed",
		"https://[64:ff9b::7f00:1]/hook":      "not allowed",
		"https://example.com/" + strings.Repeat("a", maxURLLength): "at most",
	}
	for raw, wantMsg := range invalid {
		name := raw
		if len(name) > 60 {
			name = name[:60] + "..."
		}
		t.Run("invalid "+name, func(t *testing.T) {
			err := v.Validate(raw)
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want error containing %q", raw, wantMsg)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error %v is %T, want ValidationError", err, err)
			}
			if !strings.Contains(err.Error(), wantMsg) {
				t.Fatalf("error %q does not contain %q", err, wantMsg)
			}
		})
	}
}

func TestValidate_AllowLocal(t *testing.T) {
	v := NewValidator(true)

	for _, raw := range []string{
		"http://localhost:9000/hook",
		"http://127.0.0.1:9000/hook",
		"https://192.168.1.10/hook",
	} {
		if err := v.Validate(raw); err != nil {
			t.Errorf("Validate(%q) = %v, want nil in allowLocal mode", raw, err)
		}
	}

	// Structural rules still apply in development.
	for _, raw := range []string{"", "ftp://localhost/x", "http://", "http://user:pw@localhost/"} {
		if err := v.Validate(raw); err == nil {
			t.Errorf("Validate(%q) = nil, want error even in allowLocal mode", raw)
		}
	}
}

func TestBlockedAddr(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":         false,
		"1.1.1.1":         false,
		"2606:4700::1111": false,
		"127.0.0.1":       true,
		"10.1.2.3":        true,
		"169.254.169.254": true,
		"::1":             true,
		"fe80::1%eth0":    true, // zoned addresses must not slip past the prefix checks
		"::ffff:10.0.0.1": true, // IPv4-mapped IPv6
		"224.0.0.1":       true,
		"ff02::1":         true,
		"198.19.0.1":      true,
		"2001:db8::1":     true,
	}
	for s, want := range cases {
		addr := netip.MustParseAddr(s)
		if got := BlockedAddr(addr); got != want {
			t.Errorf("BlockedAddr(%s) = %v, want %v", s, got, want)
		}
	}
	if !BlockedAddr(netip.Addr{}) {
		t.Error("BlockedAddr(zero value) = false, want true")
	}
}
