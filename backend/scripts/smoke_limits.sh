#!/usr/bin/env bash
# End-to-end test of rate limiting with the real API, worker and receiver processes against the
# real Postgres and Redis:
#
#   make up && make migrate-up && ./scripts/smoke_limits.sh
#
# Part 1 gives the API a low per-tenant ingest limit and checks 429s, headers and isolation between
# tenants. Part 2 restarts it without that limit, gives the worker a low per-endpoint delivery
# limit and checks that a burst of events reaches the endpoint at a controlled pace without losing
# or failing anything.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=true # the endpoint in part 2 is the local receiver
export DELIVERY_TIMEOUT=2s
export RETRY_SCHEDULE=none

RECEIVER="localhost:19096"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh
trap stop_all EXIT

go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/receiver ./cmd/receiver

# stop_process PID  -> stops one process started with `start`
stop_process() {
  kill "$1" 2>/dev/null || true
  wait "$1" 2>/dev/null || true
}

# ------------------------------------------------------------------------------------------------
echo "== part 1: per-tenant limit on event ingestion (1 event/s, bursts of 2)"
BASE="http://localhost:18083"
start api1 env RATE_LIMIT_EVENTS_PER_SECOND=1 HTTP_ADDR=":18083" ./bin/api
API1=$LAST_PID
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api1.log"; exit 1; }

call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Limit A"}'
KEY_A=$(field 'd["api_key"]["key"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Limit B"}'
KEY_B=$(field 'd["api_key"]["key"]')

send_event() { call POST /v1/events "$1" '{"type":"limit.test","payload":{"n":1}}'; }

send_event "$KEY_A"
expect "first event accepted" 202 "$CODE"
expect "the limit header is the burst size" 2 "$(header RateLimit-Limit)"
expect "one token used, one left" 1 "$(header RateLimit-Remaining)"
send_event "$KEY_A"
expect "second event accepted (the burst)" 202 "$CODE"
expect "no tokens left" 0 "$(header RateLimit-Remaining)"

send_event "$KEY_A"
expect "third event refused" 429 "$CODE"
expect "the error uses the standard shape" rate_limited "$(field 'd["error"]["code"]')"
expect "and says when to retry" True "$(field '"retry in" in d["error"]["message"]')"
expect "Retry-After tells the client how long to wait" True "$([[ $(header Retry-After) -ge 1 ]] && echo True || echo False)"
call GET /v1/tenant "$KEY_A"
TENANT_A=$(field 'd["id"]')
events_of_a() { docker exec "$(docker compose -f ../docker-compose.yml ps -q postgres)" psql -U tern -d tern -tA -c "SELECT count(*) FROM events WHERE tenant_id = '$TENANT_A'"; }
expect "only the two accepted events were stored, not the refused one" 2 "$(events_of_a)"

send_event "$KEY_B"
expect "another tenant is not affected by A's limit" 202 "$CODE"

REFUSED=0
for _ in $(seq 1 15); do
  call GET /v1/tenant "$KEY_A"
  [[ "$CODE" == "200" ]] || REFUSED=$((REFUSED + 1))
done
expect "other routes are not limited" 0 "$REFUSED"

sleep 1.3 # one token refills per second
send_event "$KEY_A"
expect "after waiting, events are accepted again" 202 "$CODE"
expect "and stored" 3 "$(events_of_a)"

stop_process "$API1"

# ------------------------------------------------------------------------------------------------
echo "== part 2: per-endpoint limit on deliveries (2/s, bursts of 4)"
BASE="http://localhost:18085"
start api2 env RATE_LIMIT_EVENTS_PER_SECOND=0 HTTP_ADDR=":18085" ./bin/api
start worker env RATE_LIMIT_ENDPOINT_DELIVERIES_PER_SECOND=2 ./bin/worker
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api2.log"; exit 1; }

call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Throttle Co"}'
KEY=$(field 'd["api_key"]["key"]')
create_endpoint "$KEY" "http://$RECEIVER/ok" '[]'
EP=$ENDPOINT_ID
start receiver ./bin/receiver -addr ":19096" -secret "$ENDPOINT_SECRET"
wait_for_http "http://$RECEIVER/_received" || { echo "receiver did not start"; exit 1; }

EVENTS=()
for i in $(seq 1 10); do
  call POST /v1/events "$KEY" "{\"type\":\"burst.test\",\"payload\":{\"i\":$i}}"
  EVENTS+=("$(field 'd["id"]')")
done
expect "ten events accepted in a burst (ingest is unlimited here)" 10 "${#EVENTS[@]}"

received_all() { received; [[ $(field 'len(d)') -ge 10 ]]; }
received() { BODY=$(curl -s "http://$RECEIVER/_received"); }
poll 40 received_all || echo "  (timed out waiting for the deliveries)"
received
expect "every event reached the endpoint" 10 "$(field 'len(d)')"
expect "and every request verified" 10 "$(field 'len([r for r in d if r["verified"]])')"

TIMES="[__import__('datetime').datetime.fromisoformat(r['received_at'].replace('Z', '+00:00')) for r in d]"
expect "they were spread over time, not delivered all at once" True "$(field "(lambda ts: (max(ts) - min(ts)).total_seconds() >= 2.0)($TIMES)")"
expect "the first second carried only about the burst plus a refill" True "$(field "(lambda ts: sum(1 for t in ts if (t - min(ts)).total_seconds() < 1.0) <= 7)($TIMES)")"

ALL_FINE=0
for e in "${EVENTS[@]}"; do
  call GET "/v1/deliveries/$(delivery_to "$e" "$EP")" "$KEY"
  [[ $(field 'd["status"]') == "succeeded" && $(field 'd["attempt_count"]') == "1" ]] && ALL_FINE=$((ALL_FINE + 1))
done
expect "every delivery succeeded on its first attempt (being throttled is not a failed attempt)" 10 "$ALL_FINE"

call GET "/v1/endpoints/$EP" "$KEY"
expect "the endpoint's health is untouched" 0 "$(field 'd["consecutive_failures"]')"
expect "and it is still active" active "$(field 'd["status"]')"

finish || { echo; echo "--- worker log (tail)"; tail -15 "$LOG_DIR/worker.log"; exit 1; }
