// Package stats reports on delivery health for dashboards: counts, success rate, response times
// and a time series. It only reads; the data belongs to the delivery feature.
package stats

import (
	"time"

	"github.com/google/uuid"
)

// Interval is the width of a time series bucket.
type Interval string

const (
	IntervalHour Interval = "hour"
	IntervalDay  Interval = "day"
)

// ParseInterval converts text from a request into an Interval, reporting whether it is a real one.
func ParseInterval(s string) (Interval, bool) {
	switch i := Interval(s); i {
	case IntervalHour, IntervalDay:
		return i, true
	}
	return "", false
}

// Query selects what a report covers. Since is inclusive and Until exclusive.
type Query struct {
	TenantID   uuid.UUID
	EndpointID *uuid.UUID // nil means every endpoint
	Since      time.Time
	Until      time.Time
	Interval   Interval
}

// Counts is a number of deliveries per status.
type Counts struct {
	Succeeded int64
	Failed    int64
	Pending   int64
}

func (c Counts) Total() int64 { return c.Succeeded + c.Failed + c.Pending }

// SuccessRate is the share of finished deliveries that succeeded. Deliveries still pending have no
// outcome yet and are left out. It is nil when nothing has finished, as a rate of zero would
// wrongly suggest that everything failed.
func (c Counts) SuccessRate() *float64 {
	finished := c.Succeeded + c.Failed
	if finished == 0 {
		return nil
	}
	rate := float64(c.Succeeded) / float64(finished)
	return &rate
}

// ResponseTimes describes how long customers' servers took to answer, over the attempts that got
// an HTTP response, in milliseconds.
type ResponseTimes struct {
	Attempts int64
	AvgMs    float64
	P50Ms    float64
	P95Ms    float64
}

// Bucket is the deliveries created during one interval.
type Bucket struct {
	Start time.Time
	Counts
}

// Report is the answer to a Query.
type Report struct {
	Since         time.Time
	Until         time.Time
	Interval      Interval
	Counts        Counts
	ResponseTimes ResponseTimes
	// Series has one bucket per interval from Since to Until, empty ones included, so a chart can
	// be drawn from it directly.
	Series []Bucket
}
