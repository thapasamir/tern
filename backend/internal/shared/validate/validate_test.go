package validate

import (
	"strings"
	"testing"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

func TestText(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		max      int
		required bool
		want     string
		wantErr  string // substring of the message; empty means success
	}{
		{"trims", "  Acme  ", 10, true, "Acme", ""},
		{"exactly at the limit", "abcde", 5, true, "abcde", ""},
		{"counts characters, not bytes", "ééééé", 5, true, "ééééé", ""},
		{"too long", "abcdef", 5, true, "", "name must be at most 5 characters"},
		{"too long in characters", "éééééé", 5, true, "", "at most 5 characters"},
		{"required but empty", "", 5, true, "", "name is required"},
		{"required but blank", "   \t", 5, true, "", "name is required"},
		{"optional and empty", "", 5, false, "", ""},
		{"optional and blank", "   ", 5, false, "", ""},
		{"length is checked after trimming", "  abcde  ", 5, true, "abcde", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Text("name", c.in, c.max, c.required)
			if c.wantErr == "" {
				if err != nil || got != c.want {
					t.Fatalf("Text = %q, %v; want %q, nil", got, err, c.want)
				}
				return
			}
			testutil.WantCode(t, err, apperror.CodeInvalid)
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not contain %q", err, c.wantErr)
			}
		})
	}
}
