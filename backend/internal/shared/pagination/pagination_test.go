package pagination

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
)

func TestParse(t *testing.T) {
	cursor := uuid.New()

	t.Run("defaults", func(t *testing.T) {
		p, err := Parse(httptest.NewRequest("GET", "/x", nil))
		if err != nil {
			t.Fatal(err)
		}
		if p.Limit != DefaultLimit || p.After != uuid.Max {
			t.Fatalf("got %+v, want default limit and max cursor", p)
		}
	})

	t.Run("explicit values", func(t *testing.T) {
		p, err := Parse(httptest.NewRequest("GET", "/x?limit=10&cursor="+cursor.String(), nil))
		if err != nil {
			t.Fatal(err)
		}
		if p.Limit != 10 || p.After != cursor {
			t.Fatalf("got %+v", p)
		}
	})

	for _, q := range []string{"limit=0", "limit=-1", "limit=101", "limit=abc", "cursor=nope"} {
		t.Run("rejects "+q, func(t *testing.T) {
			_, err := Parse(httptest.NewRequest("GET", "/x?"+q, nil))
			if err == nil || apperror.From(err).Code != apperror.CodeInvalid {
				t.Fatalf("Parse(%q) error = %v, want invalid_request", q, err)
			}
		})
	}
}

type item struct{ id uuid.UUID }

func idOf(i item) uuid.UUID { return i.id }

func TestNewResult(t *testing.T) {
	rows := []item{{uuid.New()}, {uuid.New()}, {uuid.New()}}
	p := Params{Limit: 2}

	t.Run("more rows than the limit yields a cursor at the last item", func(t *testing.T) {
		res := NewResult(rows, p, idOf)
		if len(res.Items) != 2 {
			t.Fatalf("got %d items, want 2", len(res.Items))
		}
		if res.NextCursor == nil || *res.NextCursor != rows[1].id {
			t.Fatalf("next cursor = %v, want %v", res.NextCursor, rows[1].id)
		}
	})

	t.Run("exactly the limit means this is the last page", func(t *testing.T) {
		res := NewResult(rows[:2], p, idOf)
		if res.NextCursor != nil || len(res.Items) != 2 {
			t.Fatalf("got %+v, want 2 items and no cursor", res)
		}
	})

	t.Run("empty", func(t *testing.T) {
		res := NewResult[item](nil, p, idOf)
		if res.NextCursor != nil || len(res.Items) != 0 {
			t.Fatalf("got %+v", res)
		}
	})
}

func TestToPage_EmptyDataIsAnArrayNotNull(t *testing.T) {
	page := ToPage(Result[item]{}, func(i item) string { return i.id.String() })
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"data":[],"next_cursor":null}` {
		t.Fatalf("got %s", b)
	}
}
