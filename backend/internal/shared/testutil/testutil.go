// Package testutil holds helpers shared by the tests of several packages.
package testutil

import (
	"io"
	"log/slog"
	"testing"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

// WantCode fails the test unless err is an application error with the given code.
func WantCode(t testing.TB, err error, code apperror.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", code)
	}
	if got := apperror.From(err).Code; got != code {
		t.Fatalf("error code = %s, want %s (err: %v)", got, code, err)
	}
}

// QuietLogger returns a logger that discards everything, to keep expected warnings and errors
// out of test output.
func QuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
