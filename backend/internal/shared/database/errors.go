package database

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

// WrapErr annotates an error from the driver or from sqlc-generated code with the operation that
// failed, so logs say what was being attempted. A "no rows" result becomes apperror.ErrNotFound
// instead, which is not a failure but an answer. A nil error stays nil.
func WrapErr(err error, op string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return apperror.ErrNotFound
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}

// RequireRow is for UPDATE and DELETE statements that must hit a row: it returns
// apperror.ErrNotFound when none was affected, and otherwise treats err like WrapErr does.
func RequireRow(affected int64, err error, op string) error {
	if err != nil {
		return WrapErr(err, op)
	}
	if affected == 0 {
		return apperror.ErrNotFound
	}
	return nil
}
