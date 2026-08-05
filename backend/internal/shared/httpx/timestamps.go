package httpx

import "time"

// UTCPtr returns a pointer to t converted to UTC, or nil if t is nil. The API always reports
// timestamps in UTC, whatever time zone the database driver happens to hand back.
func UTCPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
