package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samirthapa/tern/internal/shared/apperror"
	"github.com/samirthapa/tern/internal/shared/ptr"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

// fakeRepo returns canned answers and records the queries it received.
type fakeRepo struct {
	counts  Counts
	times   ResponseTimes
	series  []Bucket
	err     error
	queries []Query
}

func (f *fakeRepo) CountByStatus(_ context.Context, q Query) (Counts, error) {
	f.queries = append(f.queries, q)
	return f.counts, f.err
}
func (f *fakeRepo) ResponseTimes(context.Context, Query) (ResponseTimes, error) {
	return f.times, f.err
}
func (f *fakeRepo) Series(context.Context, Query) ([]Bucket, error) { return f.series, f.err }

func at(hour, minute int) time.Time { return time.Date(2026, 8, 10, hour, minute, 0, 0, time.UTC) }

func newService(now time.Time) (*Service, *fakeRepo) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	svc.now = func() time.Time { return now }
	return svc, repo
}

func TestReport_DefaultsToTheLast24HoursByTheHour(t *testing.T) {
	svc, repo := newService(at(12, 0))
	rep, err := svc.Report(context.Background(), uuid.New(), Input{})
	if err != nil {
		t.Fatal(err)
	}

	q := repo.queries[0]
	if !q.Until.Equal(at(12, 0)) || !q.Since.Equal(at(12, 0).Add(-24*time.Hour)) || q.Interval != IntervalHour {
		t.Fatalf("query = %+v, want the 24 hours up to now in hourly buckets", q)
	}
	if rep.Interval != IntervalHour || len(rep.Series) != 24 {
		t.Fatalf("interval %s, %d buckets; want hour and 24 (the range is whole hours)", rep.Interval, len(rep.Series))
	}
}

func TestReport_AHoursRangeThatStartsMidHourHasABucketForThePartialHour(t *testing.T) {
	svc, _ := newService(at(12, 30))
	rep, _ := svc.Report(context.Background(), uuid.New(), Input{})
	if len(rep.Series) != 25 {
		t.Fatalf("%d buckets, want 25: the first and the last hour are both partial", len(rep.Series))
	}
	if !rep.Series[0].Start.Equal(at(12, 0).Add(-24*time.Hour)) || !rep.Series[24].Start.Equal(at(12, 0)) {
		t.Fatalf("first %v, last %v; buckets must start on the hour", rep.Series[0].Start, rep.Series[24].Start)
	}
}

func TestReport_TheSeriesIsZeroFilledAndKeepsTheCountsInTheRightBuckets(t *testing.T) {
	svc, repo := newService(at(12, 0))
	repo.series = []Bucket{
		{Start: at(9, 0), Counts: Counts{Succeeded: 5, Failed: 1}},
		{Start: at(11, 0), Counts: Counts{Pending: 2}},
	}
	since := at(8, 0)
	rep, err := svc.Report(context.Background(), uuid.New(), Input{Since: &since})
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.Series) != 4 { // 08, 09, 10, 11
		t.Fatalf("%d buckets, want 4 (08:00 to 11:00)", len(rep.Series))
	}
	want := []Counts{{}, {Succeeded: 5, Failed: 1}, {}, {Pending: 2}}
	for i, b := range rep.Series {
		if b.Counts != want[i] || !b.Start.Equal(at(8+i, 0)) {
			t.Errorf("bucket %d = %v %+v, want %v %+v", i, b.Start, b.Counts, at(8+i, 0), want[i])
		}
	}
}

func TestReport_DailyBucketsStartAtUTCMidnight(t *testing.T) {
	svc, _ := newService(at(12, 0))
	since := time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC)
	rep, err := svc.Report(context.Background(), uuid.New(), Input{Since: &since, Interval: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Series) != 4 { // Aug 7, 8, 9, 10
		t.Fatalf("%d daily buckets, want 4", len(rep.Series))
	}
	for _, b := range rep.Series {
		if h, m, s := b.Start.Clock(); h != 0 || m != 0 || s != 0 {
			t.Fatalf("bucket starts at %v, want midnight UTC", b.Start)
		}
	}
}

func TestReport_NormalisesTimezonesToUTC(t *testing.T) {
	svc, repo := newService(at(12, 0))
	zone := time.FixedZone("NPT", 5*3600+45*60)
	since := time.Date(2026, 8, 10, 6, 0, 0, 0, zone) // 00:15 UTC
	until := time.Date(2026, 8, 10, 12, 0, 0, 0, zone)
	if _, err := svc.Report(context.Background(), uuid.New(), Input{Since: &since, Until: &until}); err != nil {
		t.Fatal(err)
	}
	if q := repo.queries[0]; q.Since.Location() != time.UTC || q.Until.Location() != time.UTC || !q.Since.Equal(since) {
		t.Fatalf("query times %v - %v, want the same instants expressed in UTC", q.Since, q.Until)
	}
}

func TestReport_PassesTheEndpointFilterAndTenantOn(t *testing.T) {
	svc, repo := newService(at(12, 0))
	tenant, endpoint := uuid.New(), uuid.New()
	if _, err := svc.Report(context.Background(), tenant, Input{EndpointID: &endpoint}); err != nil {
		t.Fatal(err)
	}
	if q := repo.queries[0]; q.TenantID != tenant || q.EndpointID == nil || *q.EndpointID != endpoint {
		t.Fatalf("query = %+v, want the tenant and endpoint passed through", q)
	}
}

func TestReport_Validation(t *testing.T) {
	svc, repo := newService(at(12, 0))
	ctx := context.Background()
	earlier, later := at(8, 0), at(10, 0)
	tooOld := at(12, 0).Add(-32 * 24 * time.Hour)

	cases := map[string]Input{
		"since after until":   {Since: &later, Until: &earlier},
		"an empty range":      {Since: &later, Until: &later},
		"since in the future": {Since: ptr.To(at(13, 0))},
		"more than 31 days":   {Since: &tooOld},
		"unknown interval":    {Interval: "minute"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Report(ctx, uuid.New(), in)
			testutil.WantCode(t, err, apperror.CodeInvalid)
		})
	}
	if len(repo.queries) != 0 {
		t.Fatal("an invalid request must not reach the repository")
	}
}

func TestReport_AMonthIsAllowed(t *testing.T) {
	svc, _ := newService(at(12, 0))
	since := at(12, 0).Add(-31 * 24 * time.Hour)
	if _, err := svc.Report(context.Background(), uuid.New(), Input{Since: &since, Interval: "day"}); err != nil {
		t.Fatalf("31 days = %v, want it accepted", err)
	}
}

func TestReport_RepositoryFailuresAreInternalErrors(t *testing.T) {
	svc, repo := newService(at(12, 0))
	repo.err = errors.New("db down")
	_, err := svc.Report(context.Background(), uuid.New(), Input{})
	testutil.WantCode(t, err, apperror.CodeInternal)
}

func TestCounts(t *testing.T) {
	c := Counts{Succeeded: 8, Failed: 2, Pending: 5}
	if c.Total() != 15 {
		t.Errorf("Total = %d, want 15", c.Total())
	}
	if rate := c.SuccessRate(); rate == nil || *rate != 0.8 {
		t.Errorf("SuccessRate = %v, want 0.8: pending deliveries have no outcome and must not count", rate)
	}
	if rate := (Counts{Pending: 3}).SuccessRate(); rate != nil {
		t.Errorf("SuccessRate = %v with nothing finished, want nil (zero would claim total failure)", *rate)
	}
	if rate := (Counts{}).SuccessRate(); rate != nil {
		t.Errorf("SuccessRate = %v with no deliveries, want nil", *rate)
	}
	if rate := (Counts{Failed: 4}).SuccessRate(); rate == nil || *rate != 0 {
		t.Errorf("SuccessRate = %v when everything failed, want exactly 0", rate)
	}
}

func TestParseInterval(t *testing.T) {
	for _, ok := range []string{"hour", "day"} {
		if _, valid := ParseInterval(ok); !valid {
			t.Errorf("ParseInterval(%q) rejected", ok)
		}
	}
	for _, bad := range []string{"", "Hour", "week", "minute"} {
		if _, valid := ParseInterval(bad); valid {
			t.Errorf("ParseInterval(%q) accepted", bad)
		}
	}
}

func TestToResponse_RoundsAndNeverEmitsNullArrays(t *testing.T) {
	rep := Report{
		Since: at(0, 0), Until: at(1, 0), Interval: IntervalHour,
		Counts:        Counts{Succeeded: 2, Failed: 1},
		ResponseTimes: ResponseTimes{Attempts: 3, AvgMs: 123.456, P50Ms: 100.04, P95Ms: 250.55},
	}
	got := toResponse(rep)
	if got.SuccessRate == nil || *got.SuccessRate != 0.6667 {
		t.Errorf("success rate = %v, want 0.6667", got.SuccessRate)
	}
	if got.ResponseTimeMs.Avg != 123.5 || got.ResponseTimeMs.P50 != 100.0 || got.ResponseTimeMs.P95 != 250.6 {
		t.Errorf("response times = %+v, want one decimal", got.ResponseTimeMs)
	}
	if got.Series == nil {
		t.Error("series must be an empty array, not null")
	}
	if got.Deliveries.Total != 3 {
		t.Errorf("total = %d, want 3", got.Deliveries.Total)
	}
}
