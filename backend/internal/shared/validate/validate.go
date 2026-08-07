// Package validate holds input checks shared by services. Failures are client-safe
// apperror.Invalid errors, so their messages can be returned to API users as they are.
package validate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

// Text trims s and checks its length in characters (not bytes). With required set an empty
// result is rejected; otherwise it is allowed. field names the input in error messages.
func Text(field, s string, max int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" && required {
		return "", apperror.Invalid(field + " is required")
	}
	if utf8.RuneCountInString(s) > max {
		return "", apperror.Invalid(fmt.Sprintf("%s must be at most %d characters", field, max))
	}
	return s, nil
}
