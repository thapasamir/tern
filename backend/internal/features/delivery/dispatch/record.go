package dispatch

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/features/delivery"
	"github.com/samirthapa/tern/internal/features/delivery/sender"
	"github.com/samirthapa/tern/internal/shared/ptr"
)

// maxErrorLen bounds the error text stored for an attempt.
const maxErrorLen = 500

// newAttemptRecord turns a send result and the verdict on it into the record to persist.
func newAttemptRecord(deliveryID uuid.UUID, res sender.Result, v verdict, started, finished time.Time) delivery.AttemptRecord {
	rec := delivery.AttemptRecord{
		DeliveryID: deliveryID,
		Outcome:    v.outcome,
		Error:      failureMessage(res),
		Duration:   res.Duration,
		StartedAt:  started,
		FinishedAt: finished,
	}
	if !v.retryAt.IsZero() {
		rec.RetryAt = ptr.To(v.retryAt)
	}
	if res.StatusCode != 0 {
		rec.StatusCode = ptr.To(res.StatusCode)
	}
	if res.Body != "" {
		rec.ResponseBody = ptr.To(res.Body)
	}
	return rec
}

// failureMessage explains why an attempt failed, or is nil if it did not.
func failureMessage(res sender.Result) *string {
	switch {
	case res.Err != nil:
		return ptr.To(truncate(sender.Sanitize(res.Err.Error()), maxErrorLen))
	case !isSuccess(res.StatusCode):
		return ptr.To(fmt.Sprintf("endpoint responded with HTTP %d", res.StatusCode))
	default:
		return nil
	}
}

// truncate shortens s to at most max bytes without splitting a UTF-8 character.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
