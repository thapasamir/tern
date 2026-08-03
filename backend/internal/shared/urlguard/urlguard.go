// Package urlguard validates customer-supplied webhook URLs and blocks SSRF targets.
//
// A webhook platform makes HTTP requests to addresses chosen by its users, so it must refuse to
// call internal services (cloud metadata at 169.254.169.254, databases on 10.x, localhost, ...).
//
// Validate checks the URL as written. That alone is NOT sufficient: a hostname can resolve to a
// private address, or change what it resolves to between validation and delivery (DNS rebinding).
// The delivery HTTP client must therefore also call BlockedAddr on the resolved IP at connect time.
package urlguard

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

const maxURLLength = 2048

// ValidationError is a client-safe validation message. Validate only ever returns this type,
// so its text can be shown to API users as-is.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

const errHostNotAllowed ValidationError = "url host is not allowed"

// Validator checks webhook URLs against the platform's policy.
type Validator struct {
	allowLocal bool
}

// NewValidator returns a Validator. With allowLocal (development only) plain http and
// private or loopback hosts are accepted, so endpoints on localhost can be tested.
func NewValidator(allowLocal bool) *Validator {
	return &Validator{allowLocal: allowLocal}
}

// Validate returns a ValidationError if raw is not an acceptable webhook URL.
func (v *Validator) Validate(raw string) error {
	if raw == "" {
		return ValidationError("url is required")
	}
	if len(raw) > maxURLLength {
		return ValidationError(fmt.Sprintf("url must be at most %d characters", maxURLLength))
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ValidationError("url is not valid")
	}
	if err := v.checkScheme(u.Scheme); err != nil {
		return err
	}
	host := u.Hostname()
	if host == "" {
		return ValidationError("url must include a host")
	}
	if u.User != nil {
		return ValidationError("url must not contain credentials")
	}
	if v.allowLocal {
		return nil
	}
	if blockedHostname(host) {
		return errHostNotAllowed
	}
	if addr, err := netip.ParseAddr(host); err == nil && BlockedAddr(addr) {
		return errHostNotAllowed
	}
	return nil
}

func (v *Validator) checkScheme(scheme string) error {
	switch {
	case scheme == "https":
		return nil
	case scheme == "http" && v.allowLocal:
		return nil
	default:
		return ValidationError("url must use https")
	}
}

// blockedPrefixes are ranges that are not covered by the netip.Addr predicates below.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes broadcast
	netip.MustParsePrefix("2001:db8::/32"),   // IPv6 documentation
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: can embed any IPv4 address
}

// BlockedAddr reports whether addr is a loopback, private, link-local, multicast, reserved
// or otherwise non-public address that webhooks must never be delivered to.
func BlockedAddr(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("") // ::ffff:127.0.0.1 is 127.0.0.1; zones break Prefix.Contains
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast():
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func blockedHostname(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal")
}
