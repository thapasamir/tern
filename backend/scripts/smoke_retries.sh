#!/usr/bin/env bash
# End-to-end test of delivery reliability with the real API, worker and receiver processes against
# the real Postgres and Redis:
#
#   make up && make migrate-up && ./scripts/smoke_retries.sh
#
# It runs a short retry schedule and checks: retries on a backoff schedule, recovery of a flaky
# endpoint, Retry-After, 410 Gone, automatic disabling, failing the pending deliveries of a
# disabled endpoint, re-enabling, and rebuilding the queue from Postgres after it lost jobs.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=true # endpoints point at the local receiver
export HTTP_ADDR=":18082"         # avoid clashing with a dev server on :8080
export DELIVERY_TIMEOUT=2s
export RETRY_SCHEDULE=1s,1s,1s    # four attempts in all, about a second apart
export AUTO_DISABLE_AFTER=6
export RECONCILE_INTERVAL=3s      # so the lost-queue case is noticed quickly

BASE="http://localhost${HTTP_ADDR}"
RECEIVER="localhost:19098"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh
trap stop_all EXIT

go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/receiver ./cmd/receiver

start api ./bin/api
start worker ./bin/worker
WORKER_PID=$LAST_PID
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api.log"; exit 1; }

# GAPS is a Python expression over a delivery (`d`): the seconds between its consecutive attempts.
GAPS="[(b - a).total_seconds() for ts in [[__import__('datetime').datetime.fromisoformat(x['started_at'].replace('Z', '+00:00')) for x in d['attempts']]] for a, b in zip(ts, ts[1:])]"

# send_event TYPE  -> sets EVENT_ID
send_event() {
  call POST /v1/events "$KEY" "{\"type\":\"$1\",\"payload\":{\"n\":1}}"
  EVENT_ID=$(field 'd["id"]')
}

# delivery_done EVENT_ID ENDPOINT_ID  -> true once that delivery is no longer pending
delivery_done() {
  call GET "/v1/deliveries/$(delivery_to "$1" "$2")" "$KEY"
  [[ $(field 'd["status"]') != "pending" ]]
}

# endpoint_field ENDPOINT_ID PYTHON_EXPR  -> reads the endpoint and evaluates the expression on it
endpoint_field() {
  call GET "/v1/endpoints/$1" "$KEY"
  field "$2"
}

echo "== setup"
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Retry Co"}'
KEY=$(field 'd["api_key"]["key"]')
SECRETS=""
# add_endpoint NAME PATH  -> creates the endpoint, remembers its secret, sets EP_<NAME>
add_endpoint() {
  create_endpoint "$KEY" "http://$RECEIVER$2" '[]'
  printf -v "EP_$1" '%s' "$ENDPOINT_ID"
  SECRETS="${SECRETS:+$SECRETS,}$ENDPOINT_SECRET"
}
add_endpoint FAIL /fail
add_endpoint FLAKY /flaky/2
add_endpoint BUSY /retry-after/3
add_endpoint WAIT /retry-after/30
add_endpoint GONE /status/410
add_endpoint OK /ok
start receiver ./bin/receiver -addr ":19098" -secret "$SECRETS"
wait_for_http "http://$RECEIVER/_received" || { echo "receiver did not start"; exit 1; }

# Each scenario gets its own event type so that only its endpoint is subscribed to it.
# (All endpoints above subscribe to everything, so isolate them by disabling the others first.)
isolate() { # isolate KEEP_ENDPOINT_ID...  -> leave only these endpoints active
  local keep=" $* "
  for id in "$EP_FAIL" "$EP_FLAKY" "$EP_BUSY" "$EP_WAIT" "$EP_GONE" "$EP_OK"; do
    if [[ "$keep" == *" $id "* ]]; then
      call POST "/v1/endpoints/$id/enable" "$KEY" >/dev/null
    else
      call POST "/v1/endpoints/$id/disable" "$KEY" >/dev/null
    fi
  done
}

echo "== retries follow the schedule and the delivery finally fails"
isolate "$EP_FAIL"
send_event a.b
EVT_FAIL=$EVENT_ID
poll 30 delivery_done "$EVT_FAIL" "$EP_FAIL" || echo "  (timed out waiting)"
call GET "/v1/deliveries/$(delivery_to "$EVT_FAIL" "$EP_FAIL")" "$KEY"
expect "the delivery failed for good" failed "$(field 'd["status"]')"
expect "four attempts were made (the first try plus three retries)" 4 "$(field 'len(d["attempts"])')"
expect "attempt_count agrees" 4 "$(field 'd["attempt_count"]')"
expect "every attempt got the endpoint's 500" "[500, 500, 500, 500]" "$(field '[a["status_code"] for a in d["attempts"]]')"
expect "retries were spaced out, not immediate" True "$(field "min($GAPS) >= 0.7")"
expect "retries were not delayed far beyond the schedule" True "$(field "max($GAPS) <= 2.0")"
expect "nothing is scheduled any more" None "$(field 'd["next_attempt_at"]')"
expect "the endpoint counted the four failed attempts" 4 "$(endpoint_field "$EP_FAIL" 'd["consecutive_failures"]')"
expect "and is still active (the limit is 6)" active "$(endpoint_field "$EP_FAIL" 'd["status"]')"

echo "== a flaky endpoint recovers on a retry"
isolate "$EP_FLAKY"
send_event flaky.event
EVT_FLAKY=$EVENT_ID
poll 30 delivery_done "$EVT_FLAKY" "$EP_FLAKY" || echo "  (timed out waiting)"
call GET "/v1/deliveries/$(delivery_to "$EVT_FLAKY" "$EP_FLAKY")" "$KEY"
expect "the delivery succeeded" succeeded "$(field 'd["status"]')"
expect "after failing twice" "[500, 500, 200]" "$(field '[a["status_code"] for a in d["attempts"]]')"
expect "no retry is pending any more" None "$(field 'd["next_attempt_at"]')"
expect "a success clears the endpoint's failure streak" 0 "$(endpoint_field "$EP_FLAKY" 'd["consecutive_failures"]')"

echo "== Retry-After is honoured"
isolate "$EP_BUSY"
send_event busy.event
EVT_BUSY=$EVENT_ID
poll 30 delivery_done "$EVT_BUSY" "$EP_BUSY" || echo "  (timed out waiting)"
call GET "/v1/deliveries/$(delivery_to "$EVT_BUSY" "$EP_BUSY")" "$KEY"
expect "answered 429 first, then 200" "[429, 200]" "$(field '[a["status_code"] for a in d["attempts"]]')"
expect "the retry waited the 3s the server asked for, not our 1s backoff" True "$(field "min($GAPS) >= 2.8")"
expect "the delivery succeeded" succeeded "$(field 'd["status"]')"

echo "== a pending retry is visible in the API"
isolate "$EP_WAIT"
send_event wait.event
EVT_WAIT=$EVENT_ID
waiting() {
  call GET "/v1/deliveries/$(delivery_to "$EVT_WAIT" "$EP_WAIT")" "$KEY"
  [[ $(field 'd["attempt_count"]') -ge 1 ]]
}
poll 10 waiting || echo "  (timed out waiting)"
expect "still pending while it waits to be retried" pending "$(field 'd["status"]')"
expect "one attempt so far" 1 "$(field 'd["attempt_count"]')"
expect "the 429 was recorded" 429 "$(field 'd["attempts"][0]["status_code"]')"
expect "the next attempt is scheduled about 30s ahead (as the server asked)" True \
  "$(field "(__import__('datetime').datetime.fromisoformat(d['next_attempt_at'].replace('Z', '+00:00')) - __import__('datetime').datetime.now(__import__('datetime').timezone.utc)).total_seconds() > 20")"

echo "== 410 Gone: no retries, the endpoint is disabled at once"
isolate "$EP_GONE"
send_event gone.event
EVT_GONE=$EVENT_ID
poll 30 delivery_done "$EVT_GONE" "$EP_GONE" || echo "  (timed out waiting)"
call GET "/v1/deliveries/$(delivery_to "$EVT_GONE" "$EP_GONE")" "$KEY"
expect "failed" failed "$(field 'd["status"]')"
expect "after a single attempt" 1 "$(field 'len(d["attempts"])')"
expect "which got the 410" 410 "$(field 'd["attempts"][0]["status_code"]')"
expect "the endpoint is disabled" disabled "$(endpoint_field "$EP_GONE" 'd["status"]')"
expect "and says why" "endpoint responded with HTTP 410 Gone" "$(endpoint_field "$EP_GONE" 'd["disabled_reason"]')"

echo "== too many failures in a row disable the endpoint and fail its pending deliveries"
isolate "$EP_FAIL"                       # re-enabling resets its failure count to 0
expect "re-enabling cleared the failure count" 0 "$(endpoint_field "$EP_FAIL" 'd["consecutive_failures"]')"
EVENTS=()
for i in 1 2 3; do
  send_event "burst.event"
  EVENTS+=("$EVENT_ID")
done
all_settled() {
  for e in "${EVENTS[@]}"; do delivery_done "$e" "$EP_FAIL" || return 1; done
}
poll 40 all_settled || echo "  (timed out waiting)"
expect "the endpoint disabled itself" disabled "$(endpoint_field "$EP_FAIL" 'd["status"]')"
expect "recording the failure count in the reason" "disabled automatically after 6 consecutive failed attempts" "$(endpoint_field "$EP_FAIL" 'd["disabled_reason"]')"
for e in "${EVENTS[@]}"; do
  call GET "/v1/deliveries/$(delivery_to "$e" "$EP_FAIL")" "$KEY"
  expect "its delivery ended failed, not stuck pending" failed "$(field 'd["status"]')"
  expect "and says why" "endpoint is disabled" "$(field 'd["attempts"][-1]["error"]')"
done
send_event "after.disable"
call GET "/v1/events/$EVENT_ID/deliveries" "$KEY"
expect "a disabled endpoint receives no new deliveries" 0 "$(field 'len(d["data"])')"
call POST "/v1/endpoints/$EP_FAIL/enable" "$KEY"
expect "re-enabling works" active "$(field 'd["status"]')"
expect "with a clean failure count" 0 "$(field 'd["consecutive_failures"]')"

echo "== the queue is rebuilt from Postgres when Redis loses jobs"
isolate "$EP_OK"
send_event reconcile.seed
EVT_SEED=$EVENT_ID
poll 20 delivery_done "$EVT_SEED" "$EP_OK" || true
POSTGRES=$(docker compose -f ../docker-compose.yml ps -q postgres)
# A pending delivery that is long overdue and marked as queued, but whose job is not in Redis:
# exactly what is left behind when Redis restarts without its data.
LOST=$(docker exec "$POSTGRES" psql -U webhook -d webhook -tA -c "
  INSERT INTO deliveries (id, tenant_id, event_id, endpoint_id, status, enqueued_at, created_at, updated_at)
  SELECT gen_random_uuid(), e.tenant_id, e.id, '$EP_OK', 'pending',
         now() - interval '10 minutes', now() - interval '10 minutes', now() - interval '10 minutes'
  FROM events e WHERE e.id = '$EVT_SEED'
  RETURNING id" | head -1)
lost_delivered() { call GET "/v1/deliveries/$LOST" "$KEY"; [[ $(field 'd["status"]') == "succeeded" ]]; }
poll 30 lost_delivered || echo "  (timed out waiting)"
expect "the reconciler re-queued it and it was delivered" succeeded "$(call GET "/v1/deliveries/$LOST" "$KEY"; field 'd["status"]')"

echo "== graceful shutdown"
kill -TERM "$WORKER_PID"
worker_stopped() { grep -q "worker stopped cleanly" "$LOG_DIR/worker.log"; }
expect "the worker exits cleanly on SIGTERM" ok "$(poll 10 worker_stopped && echo ok || echo no)"

finish || { echo; echo "--- worker log (tail)"; tail -25 "$LOG_DIR/worker.log"; echo "--- api log (tail)"; tail -10 "$LOG_DIR/api.log"; exit 1; }
