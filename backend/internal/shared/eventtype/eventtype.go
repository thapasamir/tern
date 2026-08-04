// Package eventtype defines what a valid event type looks like. Event types appear both in
// endpoint subscriptions and on incoming events, and the two must agree.
package eventtype

import "regexp"

// MaxLen is the longest accepted event type.
const MaxLen = 128

// Hint explains the format in error messages.
const Hint = `use lowercase letters, digits and . _ - (for example "invoice.paid")`

// pattern accepts dotted lowercase names such as "invoice.paid" or "user.created".
var pattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// Valid reports whether s is a well-formed event type.
func Valid(s string) bool {
	return len(s) <= MaxLen && pattern.MatchString(s)
}
