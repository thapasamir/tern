package delivery

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestToViewResponse_HasNoLastAttemptBeforeTheFirstOne(t *testing.T) {
	v := View{Delivery: Delivery{ID: uuid.New(), Status: StatusPending}, EventType: "invoice.paid", EndpointURL: "https://example.com/h"}

	body, err := json.Marshal(toViewResponse(v))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["event_type"] != "invoice.paid" || got["endpoint_url"] != "https://example.com/h" {
		t.Fatalf("response = %v, want the event type and endpoint url", got)
	}
	if last, present := got["last_attempt"]; !present || last != nil {
		t.Fatalf("last_attempt = %v (present %v), want an explicit null", last, present)
	}
}

func TestToViewResponse_SummarisesTheLastAttempt(t *testing.T) {
	code := 502
	started := time.Date(2026, 8, 24, 10, 0, 0, 0, time.FixedZone("NPT", 5*3600+45*60))
	v := View{LastAttempt: &AttemptSummary{StatusCode: &code, Duration: 1500 * time.Millisecond, StartedAt: started}}

	got := toViewResponse(v).LastAttempt
	if got == nil || *got.StatusCode != 502 || got.DurationMs != 1500 || got.StartedAt.Location() != time.UTC {
		t.Fatalf("last attempt = %+v, want status 502, 1500ms and a UTC time", got)
	}
}
