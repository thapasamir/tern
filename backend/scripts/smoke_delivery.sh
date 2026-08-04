#!/usr/bin/env bash
# End-to-end test of event delivery with the real API, worker and receiver processes against the
# real Postgres and Redis:
#
#   make up && make migrate-up && ./scripts/smoke_delivery.sh
#
# It registers endpoints that behave differently (fine, filtered by event type, failing, slow,
# answering 410, disabled), sends events, and checks what each endpoint received, what was
# recorded in the delivery log, and that signatures verify the way a customer would verify them.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=true # endpoints point at the local receiver
export HTTP_ADDR=":18081"         # avoid clashing with a dev server on :8080
export DELIVERY_TIMEOUT=1s        # so the "slow endpoint" case finishes quickly

BASE="http://localhost${HTTP_ADDR}"
RECEIVER="localhost:19099"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh

LOG_DIR=$(mktemp -d)
PIDS=()
cleanup() {
  for pid in ${PIDS[@]+"${PIDS[@]}"}; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true # reaps it, so the shell does not print "Terminated"
  done
  rm -f "$HDR_FILE"
  rm -rf "$LOG_DIR"
}
trap cleanup EXIT

# start NAME COMMAND [ARGS...]  -> runs a process in the background, logging to $LOG_DIR/NAME.log
start() {
  local name=$1
  shift
  "$@" >"$LOG_DIR/$name.log" 2>&1 &
  PIDS+=($!)
  LAST_PID=$!
}

go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/receiver ./cmd/receiver

start api ./bin/api
start worker ./bin/worker
WORKER_PID=$LAST_PID
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api.log"; exit 1; }

# create_endpoint TOKEN URL EVENT_TYPES_JSON  -> sets ENDPOINT_ID and ENDPOINT_SECRET
create_endpoint() {
  call POST /v1/endpoints "$1" "{\"url\":\"$2\",\"event_types\":$3}"
  ENDPOINT_ID=$(field 'd["id"]')
  ENDPOINT_SECRET=$(field 'd["secret"]')
}

# deliveries_settled TOKEN EVENT_ID COUNT  -> true once the event has COUNT deliveries, none pending
deliveries_settled() {
  call GET "/v1/events/$2/deliveries?limit=100" "$1"
  [[ $(field 'len(d["data"])') == "$3" && $(field 'sum(1 for x in d["data"] if x["status"] == "pending")') == "0" ]]
}

# delivery_to EVENT_ID ENDPOINT_ID  -> prints the id of the delivery of that event to that endpoint
delivery_to() {
  call GET "/v1/events/$1/deliveries?limit=100" "$KEY"
  field "next((x['id'] for x in d['data'] if x['endpoint_id'] == '$2'), 'none')"
}

# expect_delivery NAME DELIVERY_ID WANT_STATUS WANT_HTTP_CODE  -> checks one delivery and its single attempt
expect_delivery() {
  call GET "/v1/deliveries/$2" "$KEY"
  expect "$1: status" "$3" "$(field 'd["status"]')"
  expect "$1: one attempt recorded" 1 "$(field 'len(d["attempts"])')"
  expect "$1: attempt_count" 1 "$(field 'd["attempt_count"]')"
  expect "$1: http status of the attempt" "$4" "$(field 'd["attempts"][0]["status_code"]')"
}

# received  -> loads what the receiver has recorded into BODY
received() { BODY=$(curl -s "http://$RECEIVER/_received"); }

echo "== setup: a tenant and six endpoints"
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Delivery Co"}'
KEY=$(field 'd["api_key"]["key"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Other Co"}'
OTHER_KEY=$(field 'd["api_key"]["key"]')

create_endpoint "$KEY" "http://$RECEIVER/ok" '[]';                    EP_OK=$ENDPOINT_ID;      SECRETS="$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/invoice" '["invoice.paid"]'; EP_INVOICE=$ENDPOINT_ID; SECRETS="$SECRETS,$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/fail" '[]';                  EP_FAIL=$ENDPOINT_ID;    SECRETS="$SECRETS,$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/slow?ms=3000" '[]';          EP_SLOW=$ENDPOINT_ID;    SECRETS="$SECRETS,$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/status/410" '[]';            EP_GONE=$ENDPOINT_ID;    SECRETS="$SECRETS,$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/disabled" '[]';              EP_DISABLED=$ENDPOINT_ID; SECRETS="$SECRETS,$ENDPOINT_SECRET"
call POST "/v1/endpoints/$EP_DISABLED/disable" "$KEY"
expect "disabled endpoint is disabled" disabled "$(field 'd["status"]')"

start receiver ./bin/receiver -addr ":19099" -secret "$SECRETS"
wait_for_http "http://$RECEIVER/_received" || { echo "receiver did not start"; exit 1; }

echo "== send an invoice.paid event"
call POST /v1/events "$KEY" '{"type":"invoice.paid","payload":{"amount":42,"currency":"usd"}}'
expect "event accepted" 202 "$CODE"
EVT1=$(field 'd["id"]')
expect "response echoes the type" invoice.paid "$(field 'd["type"]')"
expect "response echoes the payload" "{'amount': 42, 'currency': 'usd'}" "$(field 'd["payload"]')"

# five of the six endpoints match: the disabled one is skipped
poll 30 deliveries_settled "$KEY" "$EVT1" 5 || { echo "deliveries did not settle"; tail -20 "$LOG_DIR/worker.log"; }
expect "five deliveries, none pending (disabled endpoint skipped)" 0 "$(deliveries_settled "$KEY" "$EVT1" 5 && echo 0 || echo 1)"
expect "no delivery to the disabled endpoint" none "$(delivery_to "$EVT1" "$EP_DISABLED")"

echo "== delivery log"
expect_delivery "ok endpoint" "$(delivery_to "$EVT1" "$EP_OK")" succeeded 200
expect_delivery "type-filtered endpoint" "$(delivery_to "$EVT1" "$EP_INVOICE")" succeeded 200
expect_delivery "failing endpoint" "$(delivery_to "$EVT1" "$EP_FAIL")" failed 500
expect "failing endpoint: error explains" "endpoint responded with HTTP 500" "$(field 'd["attempts"][0]["error"]')"
expect_delivery "410 endpoint" "$(delivery_to "$EVT1" "$EP_GONE")" failed 410
expect_delivery "slow endpoint" "$(delivery_to "$EVT1" "$EP_SLOW")" failed None
expect "slow endpoint: recorded as a timeout" True "$(field '"deadline" in d["attempts"][0]["error"].lower() or "timeout" in d["attempts"][0]["error"].lower()')"
expect "slow endpoint: no status code" None "$(field 'd["attempts"][0]["status_code"]')"

echo "== what the receiver actually got"
received
expect "the ok endpoint received the request, signature valid" 1 "$(field 'len([r for r in d if r["path"] == "/ok" and r["verified"]])')"
expect "the filtered endpoint received it too" 1 "$(field 'len([r for r in d if r["path"] == "/invoice" and r["verified"]])')"
expect "the failing endpoint received it with a valid signature" 1 "$(field 'len([r for r in d if r["path"] == "/fail" and r["verified"]])')"
expect "the slow endpoint received it with a valid signature" 1 "$(field 'len([r for r in d if r["path"] == "/slow" and r["verified"]])')"
expect "no request failed verification" 0 "$(field 'len([r for r in d if not r["verified"]])')"
expect "the disabled endpoint received nothing" 0 "$(field 'len([r for r in d if r["path"] == "/disabled"])')"
expect "webhook-id is the event id" True "$(field "all(r['webhook_id'] == '$EVT1' for r in d)")"
expect "envelope: type" invoice.paid "$(field '[r for r in d if r["path"] == "/ok"][0]["body"]["type"]')"
expect "envelope: id" "$EVT1" "$(field '[r for r in d if r["path"] == "/ok"][0]["body"]["id"]')"
expect "envelope: data is the payload" "{'amount': 42, 'currency': 'usd'}" "$(field '[r for r in d if r["path"] == "/ok"][0]["body"]["data"]')"

echo "== event type filtering"
call POST /v1/events "$KEY" '{"type":"user.created","payload":{"id":7}}'
EVT2=$(field 'd["id"]')
poll 30 deliveries_settled "$KEY" "$EVT2" 4 || true
expect "user.created skips the invoice-only endpoint" 0 "$(deliveries_settled "$KEY" "$EVT2" 4 && echo 0 || echo 1)"
expect "invoice endpoint not among the deliveries" False "$(field "'$EP_INVOICE' in [x['endpoint_id'] for x in d['data']]")"

echo "== idempotency"
call POST /v1/events "$KEY" '{"type":"order.created","payload":{"n":1}}' -H "Idempotency-Key: order-1001"
expect "first send accepted" 202 "$CODE"
IDEM=$(field 'd["id"]')
call POST /v1/events "$KEY" '{"type":"order.created","payload":{"n":1}}' -H "Idempotency-Key: order-1001"
expect "replay answers 200" 200 "$CODE"
expect "replay returns the original event" "$IDEM" "$(field 'd["id"]')"
expect "replay is flagged" true "$(header Idempotent-Replayed)"
poll 30 deliveries_settled "$KEY" "$IDEM" 4 || true
expect "the event fanned out exactly once" 0 "$(deliveries_settled "$KEY" "$IDEM" 4 && echo 0 || echo 1)"
call POST /v1/events "$OTHER_KEY" '{"type":"order.created","payload":{"n":1}}' -H "Idempotency-Key: order-1001"
expect "another tenant may reuse the key" 202 "$CODE"

echo "== validation"
call POST /v1/events "$KEY" '{"type":"Bad Type","payload":{}}';                  expect "malformed type" 400 "$CODE"
call POST /v1/events "$KEY" '{"type":"a.b"}';                                    expect "missing payload" 400 "$CODE"
call POST /v1/events "$KEY" '{"type":"a.b","payload":[1,2]}';                    expect "array payload" 400 "$CODE"
call POST /v1/events "$KEY" '{"type":"a.b","payload":null}';                     expect "null payload" 400 "$CODE"
call POST /v1/events "$KEY" '{"type":"a.b","payload":{},"extra":1}';             expect "unknown field" 400 "$CODE"
call POST /v1/events "$KEY" '{"type":"a.b","payload":{}}' -H "Idempotency-Key: $(printf 'k%.0s' $(seq 1 300))"
expect "oversized idempotency key" 400 "$CODE"
call POST /v1/events "" '{"type":"a.b","payload":{}}';                           expect "no api key" 401 "$CODE"

echo "== tenant isolation"
call GET "/v1/events/$EVT1" "$KEY";                      expect "owner reads the event" 200 "$CODE"
call GET "/v1/events/$EVT1" "$OTHER_KEY";                expect "another tenant cannot read the event" 404 "$CODE"
call GET "/v1/deliveries/$(delivery_to "$EVT1" "$EP_OK")" "$OTHER_KEY"
expect "another tenant cannot read the delivery" 404 "$CODE"
call GET "/v1/events/$EVT1/deliveries" "$OTHER_KEY"
expect "another tenant sees no deliveries of it" 0 "$(field 'len(d["data"])')"
call POST /v1/events "$OTHER_KEY" '{"type":"x.y","payload":{}}'
OTHER_EVT=$(field 'd["id"]')
expect "a tenant without endpoints can still send" 202 "$CODE"
call GET "/v1/events/$OTHER_EVT/deliveries" "$OTHER_KEY"
expect "and gets no deliveries" 0 "$(field 'len(d["data"])')"
call GET "/v1/events/not-a-uuid" "$KEY";                 expect "bad event id" 400 "$CODE"
call GET "/v1/deliveries/$(uuidgen | tr A-Z a-z)" "$KEY"; expect "unknown delivery" 404 "$CODE"

echo "== pagination of the delivery list"
call GET "/v1/events/$EVT1/deliveries?limit=2" "$KEY"
expect "first page" 2 "$(field 'len(d["data"])')"
CURSOR=$(field 'd["next_cursor"]')
call GET "/v1/events/$EVT1/deliveries?limit=2&cursor=$CURSOR" "$KEY"
expect "second page" 2 "$(field 'len(d["data"])')"
CURSOR=$(field 'd["next_cursor"]')
call GET "/v1/events/$EVT1/deliveries?limit=2&cursor=$CURSOR" "$KEY"
expect "last page" 1 "$(field 'len(d["data"])')"
expect "no further cursor" None "$(field 'd["next_cursor"]')"

echo "== outbox sweeper: a delivery committed but never queued (simulates a crash before the Redis write)"
POSTGRES=$(docker compose -f ../docker-compose.yml ps -q postgres)
ORPHAN=$(docker exec "$POSTGRES" psql -U webhook -d webhook -tA -c "
  INSERT INTO deliveries (id, tenant_id, event_id, endpoint_id, status, created_at, updated_at)
  SELECT gen_random_uuid(), e.tenant_id, e.id, '$EP_OK', 'pending', now() - interval '2 minutes', now() - interval '2 minutes'
  FROM events e WHERE e.id = '$EVT1'
  RETURNING id" | head -1)
orphan_delivered() { call GET "/v1/deliveries/$ORPHAN" "$KEY"; [[ $(field 'd["status"]') == "succeeded" ]]; }
poll 45 orphan_delivered || true
expect "the sweeper queued the orphan and the worker delivered it" succeeded "$(call GET "/v1/deliveries/$ORPHAN" "$KEY"; field 'd["status"]')"
expect "the orphan is now marked as enqueued" t "$(docker exec "$POSTGRES" psql -U webhook -d webhook -tA -c "SELECT enqueued_at IS NOT NULL FROM deliveries WHERE id = '$ORPHAN'")"

echo "== graceful shutdown"
kill -TERM "$WORKER_PID"
worker_stopped() { grep -q "worker stopped cleanly" "$LOG_DIR/worker.log"; }
expect "the worker exits cleanly on SIGTERM" ok "$(poll 10 worker_stopped && echo ok || echo no)"

finish || { echo; echo "--- worker log (tail)"; tail -15 "$LOG_DIR/worker.log"; echo "--- api log (tail)"; tail -10 "$LOG_DIR/api.log"; exit 1; }
