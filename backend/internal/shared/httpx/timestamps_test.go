package httpx

import (
	"testing"
	"time"
)

func TestUTCPtr(t *testing.T) {
	if UTCPtr(nil) != nil {
		t.Fatal("UTCPtr(nil) must be nil")
	}

	zone := time.FixedZone("NPT", 5*3600+45*60)
	local := time.Date(2026, 8, 1, 12, 0, 0, 0, zone)
	got := UTCPtr(&local)

	if got.Location() != time.UTC || !got.Equal(local) {
		t.Fatalf("UTCPtr(%v) = %v, want the same instant in UTC", local, got)
	}
	if local.Location() != zone {
		t.Fatal("the input must not be modified")
	}
}
