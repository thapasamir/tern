// Package pagination implements cursor pagination for time-ordered (UUIDv7) IDs.
//
// Lists are returned newest first. The cursor is the ID of the last item on the previous page,
// so the next page is "rows with a smaller ID". Unlike OFFSET, this stays fast on deep pages and
// does not skip or repeat rows when new ones are inserted while a client is paging.
package pagination

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

const (
	DefaultLimit int32 = 50
	MaxLimit     int32 = 100
)

// Params are the validated pagination inputs of a list request.
type Params struct {
	Limit int32
	// After is exclusive: only rows with ID < After are returned. For the first page it is the
	// largest possible UUID, which keeps queries free of "is there a cursor?" branches.
	After uuid.UUID
}

// Parse reads the "limit" and "cursor" query parameters.
func Parse(r *http.Request) (Params, error) {
	q := r.URL.Query()
	p := Params{Limit: DefaultLimit, After: uuid.Max}

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > int(MaxLimit) {
			return Params{}, apperror.Invalid(fmt.Sprintf("limit must be between 1 and %d", MaxLimit))
		}
		p.Limit = int32(n)
	}
	if raw := q.Get("cursor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return Params{}, apperror.Invalid("invalid cursor")
		}
		p.After = id
	}
	return p, nil
}

// QueryLimit is the number of rows a repository should fetch: one more than the page size,
// so NewResult can tell whether another page exists without a COUNT query.
func (p Params) QueryLimit() int32 { return p.Limit + 1 }

// Result is one page of items plus the cursor of the next page, if any.
type Result[T any] struct {
	Items      []T
	NextCursor *uuid.UUID
}

// NewResult trims rows (fetched with QueryLimit) to the page size and sets the next cursor.
func NewResult[T any](rows []T, p Params, idOf func(T) uuid.UUID) Result[T] {
	if int32(len(rows)) <= p.Limit {
		return Result[T]{Items: rows}
	}
	items := rows[:p.Limit]
	next := idOf(items[len(items)-1])
	return Result[T]{Items: items, NextCursor: &next}
}

// Page is the JSON shape of a paginated response.
type Page[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// ToPage converts a Result of domain items into a Page of response DTOs.
func ToPage[S, T any](res Result[S], convert func(S) T) Page[T] {
	page := Page[T]{Data: make([]T, 0, len(res.Items))} // never null in JSON
	for _, item := range res.Items {
		page.Data = append(page.Data, convert(item))
	}
	if res.NextCursor != nil {
		c := res.NextCursor.String()
		page.NextCursor = &c
	}
	return page
}
