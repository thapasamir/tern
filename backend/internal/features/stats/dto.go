package stats

import (
	"math"
	"time"
)

type reportResponse struct {
	Since      time.Time      `json:"since"`
	Until      time.Time      `json:"until"`
	Interval   Interval       `json:"interval"`
	Deliveries countsResponse `json:"deliveries"`
	// SuccessRate is succeeded / (succeeded + failed), or null when nothing has finished yet.
	SuccessRate    *float64              `json:"success_rate"`
	ResponseTimeMs responseTimesResponse `json:"response_time_ms"`
	Series         []bucketResponse      `json:"series"`
}

type countsResponse struct {
	Total     int64 `json:"total"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Pending   int64 `json:"pending"`
}

type responseTimesResponse struct {
	Attempts int64   `json:"attempts"`
	Avg      float64 `json:"avg"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
}

type bucketResponse struct {
	Start     time.Time `json:"start"`
	Succeeded int64     `json:"succeeded"`
	Failed    int64     `json:"failed"`
	Pending   int64     `json:"pending"`
}

func toResponse(r Report) reportResponse {
	series := make([]bucketResponse, 0, len(r.Series)) // JSON [] rather than null
	for _, b := range r.Series {
		series = append(series, bucketResponse{
			Start: b.Start.UTC(), Succeeded: b.Succeeded, Failed: b.Failed, Pending: b.Pending,
		})
	}

	var rate *float64
	if sr := r.Counts.SuccessRate(); sr != nil {
		rounded := round(*sr, 4)
		rate = &rounded
	}
	return reportResponse{
		Since:       r.Since.UTC(),
		Until:       r.Until.UTC(),
		Interval:    r.Interval,
		Deliveries:  countsResponse{Total: r.Counts.Total(), Succeeded: r.Counts.Succeeded, Failed: r.Counts.Failed, Pending: r.Counts.Pending},
		SuccessRate: rate,
		ResponseTimeMs: responseTimesResponse{
			Attempts: r.ResponseTimes.Attempts,
			Avg:      round(r.ResponseTimes.AvgMs, 1),
			P50:      round(r.ResponseTimes.P50Ms, 1),
			P95:      round(r.ResponseTimes.P95Ms, 1),
		},
		Series: series,
	}
}

func round(v float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(v*scale) / scale
}
