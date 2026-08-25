//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/samirthapa/tern/internal/features/retention"
)

func TestEventIsDeliveredSignedAndLogged(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Happy path")
	rc := newReceiver(t)
	epID := rc.endpointFor(s, key)

	eventID := s.sendEvent(key, "invoice.paid")
	eventually(t, "the delivery to finish", func() bool { return s.settled(key, eventID, 1) })

	d := s.deliveriesOf(key, eventID)[0].(map[string]any)
	if d["status"] != "succeeded" || d["endpoint_id"] != epID {
		t.Fatalf("delivery = %v, want succeeded to endpoint %s", d, epID)
	}
	if rc.received() != 1 {
		t.Fatalf("receiver got %d requests, want 1", rc.received())
	}
	if n := rc.invalidSignatures(); n != 0 {
		t.Fatalf("%d requests had an invalid signature", n)
	}

	detail := s.call(http.MethodGet, "/v1/deliveries/"+d["id"].(string), key, nil)
	attempts, _ := detail.get("attempts").([]any)
	if len(attempts) != 1 || attempts[0].(map[string]any)["status_code"] != float64(200) {
		t.Fatalf("attempts = %v, want one with status 200", attempts)
	}
}

func TestSigningSecretIsStoredEncrypted(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Encryption")
	id, secret := s.newEndpoint(key, "http://127.0.0.1:1/unused")

	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT secret FROM endpoints WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == secret || len(stored) < 8 || stored[:7] != "enc:v1:" {
		t.Fatalf("stored secret %q is not an encrypted form of the one the API returned", stored)
	}
	if opened, err := s.secrets.Open(stored); err != nil || opened != secret {
		t.Fatalf("stored secret opens to %q, %v; want %q", opened, err, secret)
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	s := startStack(t, nil)
	keyA, keyB := s.newTenant("Tenant A"), s.newTenant("Tenant B")
	rcA, rcB := newReceiver(t), newReceiver(t)
	epA := rcA.endpointFor(s, keyA)
	rcB.endpointFor(s, keyB)

	eventID := s.sendEvent(keyA, "thing.happened")
	eventually(t, "tenant A's delivery", func() bool { return s.settled(keyA, eventID, 1) })

	if rcB.received() != 0 {
		t.Fatal("tenant B's endpoint received tenant A's event")
	}
	deliveryID := s.deliveriesOf(keyA, eventID)[0].(map[string]any)["id"].(string)
	for name, path := range map[string]string{
		"endpoint": "/v1/endpoints/" + epA,
		"delivery": "/v1/deliveries/" + deliveryID,
	} {
		if got := s.call(http.MethodGet, path, keyB, nil).Status; got != http.StatusNotFound {
			t.Errorf("tenant B reading tenant A's %s: status %d, want 404", name, got)
		}
	}
	if got := s.deliveriesOf(keyB, eventID); len(got) != 0 {
		t.Errorf("tenant B lists %d deliveries of tenant A's event", len(got))
	}
}

func TestFailedDeliveryIsRetriedUntilItSucceeds(t *testing.T) {
	s := startStack(t, []time.Duration{50 * time.Millisecond, 50 * time.Millisecond})
	key := s.newTenant("Retries")
	rc := newReceiver(t)
	rc.answerWith(http.StatusInternalServerError)
	rc.endpointFor(s, key)

	eventID := s.sendEvent(key, "retry.me")
	eventually(t, "the first failed attempt", func() bool { return rc.received() >= 1 })
	rc.answerWith(http.StatusOK)
	eventually(t, "the delivery to finish", func() bool { return s.settled(key, eventID, 1) })

	d := s.deliveriesOf(key, eventID)[0].(map[string]any)
	if d["status"] != "succeeded" || d["attempt_count"].(float64) < 2 {
		t.Fatalf("delivery = %v, want succeeded after at least two attempts", d)
	}
}

func TestEndpointThatIsGoneIsDisabledAtOnce(t *testing.T) {
	s := startStack(t, []time.Duration{time.Hour})
	key := s.newTenant("Gone")
	rc := newReceiver(t)
	rc.answerWith(http.StatusGone)
	epID := rc.endpointFor(s, key)

	eventID := s.sendEvent(key, "to.nowhere")
	eventually(t, "the delivery to fail for good", func() bool { return s.settled(key, eventID, 1) })

	ep := s.call(http.MethodGet, "/v1/endpoints/"+epID, key, nil)
	if ep.str("status") != "disabled" {
		t.Fatalf("endpoint status = %q, want disabled after a 410", ep.str("status"))
	}
	if d := s.deliveriesOf(key, eventID)[0].(map[string]any); d["status"] != "failed" {
		t.Fatalf("delivery status = %v, want failed (no retries for a gone endpoint)", d["status"])
	}
}

func TestReplayCreatesALinkedDelivery(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Replay")
	rc := newReceiver(t)
	rc.answerWith(http.StatusInternalServerError)
	rc.endpointFor(s, key)

	eventID := s.sendEvent(key, "replay.me")
	eventually(t, "the delivery to fail", func() bool { return s.settled(key, eventID, 1) })
	original := s.deliveriesOf(key, eventID)[0].(map[string]any)["id"].(string)

	rc.answerWith(http.StatusOK)
	replay := s.call(http.MethodPost, "/v1/deliveries/"+original+"/replay", key, nil)
	if replay.Status != http.StatusAccepted || replay.str("replayed_from") != original {
		t.Fatalf("replay = %d %v, want 202 linked to %s", replay.Status, replay.Body, original)
	}
	eventually(t, "the replay to finish", func() bool {
		return s.call(http.MethodGet, "/v1/deliveries/"+replay.str("id"), key, nil).str("status") == "succeeded"
	})
	if got := s.call(http.MethodGet, "/v1/deliveries/"+original, key, nil).str("status"); got != "failed" {
		t.Fatalf("original delivery status = %q, want it to stay failed", got)
	}
}

func TestIdempotencyKeyDeduplicatesEvents(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Idempotency")
	rc := newReceiver(t)
	rc.endpointFor(s, key)

	body := map[string]any{"type": "once.only", "payload": map[string]any{}}
	first := s.call(http.MethodPost, "/v1/events", key, body, "Idempotency-Key", "abc-123")
	second := s.call(http.MethodPost, "/v1/events", key, body, "Idempotency-Key", "abc-123")

	if first.Status != http.StatusAccepted || second.Status != http.StatusOK {
		t.Fatalf("statuses = %d then %d, want 202 then 200", first.Status, second.Status)
	}
	if first.str("id") != second.str("id") {
		t.Fatalf("the repeated request created a second event: %s vs %s", first.str("id"), second.str("id"))
	}
	eventually(t, "the delivery to finish", func() bool { return s.settled(key, first.str("id"), 1) })
	if rc.received() != 1 {
		t.Fatalf("receiver got %d requests, want exactly 1", rc.received())
	}
}

func TestDeliveryLogFiltersAndStats(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Log and stats")
	good, bad := newReceiver(t), newReceiver(t)
	bad.answerWith(http.StatusBadGateway)
	goodEP := good.endpointFor(s, key)
	badEP := bad.endpointFor(s, key)

	eventID := s.sendEvent(key, "mixed.results")
	eventually(t, "both deliveries to finish", func() bool { return s.settled(key, eventID, 2) })

	failed := s.call(http.MethodGet, "/v1/deliveries?status=failed", key, nil)
	data, _ := failed.get("data").([]any)
	if len(data) != 1 || data[0].(map[string]any)["endpoint_id"] != badEP {
		t.Fatalf("status=failed returned %v, want only the delivery to %s", data, badEP)
	}
	// A log row says what was sent, where, and how the latest attempt went, with no second call.
	row := data[0].(map[string]any)
	last, _ := row["last_attempt"].(map[string]any)
	if row["event_type"] != "mixed.results" || row["endpoint_url"] == "" || last["status_code"] != float64(502) {
		t.Fatalf("log row = %v, want event type, endpoint url and a last attempt answered with 502", row)
	}
	onlyGood := s.call(http.MethodGet, "/v1/deliveries?endpoint_id="+goodEP, key, nil)
	if data, _ := onlyGood.get("data").([]any); len(data) != 1 || data[0].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("endpoint_id filter returned %v", data)
	}
	if got := s.call(http.MethodGet, "/v1/deliveries?status=bogus", key, nil).Status; got != http.StatusBadRequest {
		t.Fatalf("invalid status filter: status %d, want 400", got)
	}

	stats := s.call(http.MethodGet, "/v1/stats/deliveries", key, nil)
	if stats.get("deliveries", "total") != float64(2) || stats.get("success_rate") != 0.5 {
		t.Fatalf("stats = %v, want 2 deliveries and a success rate of 0.5", stats.Body)
	}
}

func TestRetentionDeletesOldEventsWithTheirDeliveries(t *testing.T) {
	s := startStack(t, nil)
	key := s.newTenant("Retention")
	rc := newReceiver(t)
	rc.endpointFor(s, key)

	oldEvent := s.sendEvent(key, "old.news")
	keptEvent := s.sendEvent(key, "fresh.news")
	eventually(t, "both deliveries to finish", func() bool {
		return s.settled(key, oldEvent, 1) && s.settled(key, keptEvent, 1)
	})

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE events SET created_at = now() - interval '40 days' WHERE id = $1`, oldEvent); err != nil {
		t.Fatal(err)
	}
	purged, err := retention.NewService(retention.NewRepository(pool), 30*24*time.Hour).Purge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if purged < 1 {
		t.Fatalf("purged %d events, want at least the old one", purged)
	}

	if got := len(s.deliveriesOf(key, oldEvent)); got != 0 {
		t.Errorf("the old event still has %d deliveries", got)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM delivery_attempts a JOIN deliveries d ON d.id = a.delivery_id WHERE d.event_id = $1`, oldEvent).Scan(&attempts); err != nil || attempts != 0 {
		t.Errorf("attempts of the old event: %d (err %v), want 0", attempts, err)
	}
	if got := len(s.deliveriesOf(key, keptEvent)); got != 1 {
		t.Errorf("the recent event has %d deliveries, want 1 (it must survive)", got)
	}
}
