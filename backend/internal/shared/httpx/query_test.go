package httpx

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

func TestQueryUUID(t *testing.T) {
	id := uuid.New()

	got, err := QueryUUID(httptest.NewRequest("GET", "/x?endpoint_id="+id.String(), nil), "endpoint_id")
	if err != nil || got == nil || *got != id {
		t.Fatalf("QueryUUID = %v, %v; want %s", got, err, id)
	}

	got, err = QueryUUID(httptest.NewRequest("GET", "/x", nil), "endpoint_id")
	if err != nil || got != nil {
		t.Fatalf("an absent parameter must give nil, got %v, %v", got, err)
	}

	_, err = QueryUUID(httptest.NewRequest("GET", "/x?endpoint_id=nope", nil), "endpoint_id")
	testutil.WantCode(t, err, apperror.CodeInvalid)
	if !strings.Contains(err.Error(), "endpoint_id") {
		t.Fatalf("error %q should name the parameter", err)
	}
}

func TestQueryTime(t *testing.T) {
	want := time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)

	got, err := QueryTime(httptest.NewRequest("GET", "/x?since=2026-08-01T12:30:00Z", nil), "since")
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("QueryTime = %v, %v; want %v", got, err, want)
	}

	got, err = QueryTime(httptest.NewRequest("GET", "/x?since=2026-08-01T18:15:00%2B05:45", nil), "since")
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("an offset timestamp = %v, %v; want the same instant %v", got, err, want)
	}

	got, err = QueryTime(httptest.NewRequest("GET", "/x", nil), "since")
	if err != nil || got != nil {
		t.Fatalf("an absent parameter must give nil, got %v, %v", got, err)
	}

	for _, bad := range []string{"yesterday", "2026-08-01", "1700000000", "2026-08-01 12:30:00"} {
		_, err = QueryTime(httptest.NewRequest("GET", "/x?since="+url.QueryEscape(bad), nil), "since")
		testutil.WantCode(t, err, apperror.CodeInvalid)
	}
}
