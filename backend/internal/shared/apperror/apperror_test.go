package apperror

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestFromStore(t *testing.T) {
	t.Run("not found carries the caller's message", func(t *testing.T) {
		e := FromStore(ErrNotFound, "endpoint not found")
		if e.Code != CodeNotFound || e.Message != "endpoint not found" || e.HTTPStatus() != http.StatusNotFound {
			t.Fatalf("got %+v, want a 404 \"endpoint not found\"", e)
		}
	})

	t.Run("not found is recognised through wrapping", func(t *testing.T) {
		wrapped := fmt.Errorf("lookup failed: %w", ErrNotFound)
		if got := FromStore(wrapped, "x not found").Code; got != CodeNotFound {
			t.Fatalf("code = %s, want not_found", got)
		}
	})

	t.Run("anything else is internal and hides the cause", func(t *testing.T) {
		cause := errors.New("pq: connection refused")
		e := FromStore(cause, "endpoint not found")
		if e.Code != CodeInternal || e.HTTPStatus() != http.StatusInternalServerError {
			t.Fatalf("got %+v, want a 500", e)
		}
		if e.Message == cause.Error() || !errors.Is(e, cause) {
			t.Fatal("the cause must be kept for logging but kept out of the client message")
		}
	})
}

func TestFrom(t *testing.T) {
	ae := Invalid("bad")
	if got := From(fmt.Errorf("wrapped: %w", ae)); got != ae {
		t.Fatal("From must unwrap an application error")
	}
	if got := From(errors.New("boom")); got.Code != CodeInternal {
		t.Fatalf("unknown errors must become internal, got %s", got.Code)
	}
}
