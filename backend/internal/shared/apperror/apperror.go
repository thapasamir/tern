// Package apperror defines typed application errors that map cleanly to HTTP statuses.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type Code string

const (
	CodeInvalid      Code = "invalid_request"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeRateLimited  Code = "rate_limited"
	CodeInternal     Code = "internal_error"
)

// Error is an application error carrying a stable code and a client-safe message.
type Error struct {
	Code    Code
	Message string
	Err     error // underlying cause, never sent to clients
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) HTTPStatus() int {
	switch e.Code {
	case CodeInvalid:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

func Invalid(msg string) *Error      { return New(CodeInvalid, msg) }
func Unauthorized(msg string) *Error { return New(CodeUnauthorized, msg) }
func Forbidden(msg string) *Error    { return New(CodeForbidden, msg) }
func NotFound(msg string) *Error     { return New(CodeNotFound, msg) }
func Conflict(msg string) *Error     { return New(CodeConflict, msg) }

// Internal wraps an unexpected error; the cause is logged but not exposed.
func Internal(err error) *Error {
	return &Error{Code: CodeInternal, Message: "internal server error", Err: err}
}

// From converts any error into an *Error, treating unknown errors as internal.
func From(err error) *Error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err)
}
