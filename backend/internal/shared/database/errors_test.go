package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

func TestWrapErr(t *testing.T) {
	if WrapErr(nil, "get thing") != nil {
		t.Fatal("a nil error must stay nil")
	}
	if !errors.Is(WrapErr(pgx.ErrNoRows, "get thing"), apperror.ErrNotFound) {
		t.Fatal("pgx.ErrNoRows must become apperror.ErrNotFound")
	}
	if !errors.Is(WrapErr(fmt.Errorf("scan: %w", pgx.ErrNoRows), "get thing"), apperror.ErrNotFound) {
		t.Fatal("a wrapped pgx.ErrNoRows must become apperror.ErrNotFound too")
	}

	cause := errors.New("connection refused")
	got := WrapErr(cause, "get thing")
	if !errors.Is(got, cause) || !strings.HasPrefix(got.Error(), "get thing: ") {
		t.Fatalf("got %q, want the cause wrapped with the operation name", got)
	}
	if errors.Is(got, apperror.ErrNotFound) {
		t.Fatal("an ordinary failure must not look like not-found")
	}
}

func TestRequireRow(t *testing.T) {
	if err := RequireRow(1, nil, "update thing"); err != nil {
		t.Fatalf("one row affected is success, got %v", err)
	}
	if err := RequireRow(3, nil, "update thing"); err != nil {
		t.Fatalf("several rows affected is success, got %v", err)
	}
	if !errors.Is(RequireRow(0, nil, "update thing"), apperror.ErrNotFound) {
		t.Fatal("no rows affected must be not-found")
	}

	cause := errors.New("deadlock detected")
	if err := RequireRow(0, cause, "update thing"); !errors.Is(err, cause) || errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("a driver error must win over the row count, got %v", err)
	}
}
