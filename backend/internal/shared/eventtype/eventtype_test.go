package eventtype

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	valid := []string{"invoice.paid", "user.created", "a", "order_item-shipped", "v2.user.deleted", "x" + strings.Repeat("a", MaxLen-1)}
	for _, s := range valid {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}

	invalid := []string{"", " ", "Invoice.Paid", "invoice paid", ".invoice", "invoice.", "invoice..paid", "-x", "a/b", "ünï", strings.Repeat("a", MaxLen+1)}
	for _, s := range invalid {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
