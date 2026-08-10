#!/usr/bin/env bash
# End-to-end test of replay with the real API, worker and receiver processes against the real
# Postgres and Redis:
#
#   make up && make migrate-up && ./scripts/smoke_replay.sh
#
# The receiver's /flaky/1 path fails the first request it sees for each event and accepts the next,
# which is exactly what replay is for: a delivery that failed, then the same event sent again.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=true # endpoints point at the local receiver
export HTTP_ADDR=":18084"         # avoid clashing with a dev server on :8080
export DELIVERY_TIMEOUT=2s
export RETRY_SCHEDULE=none        # a failed delivery stays failed, so there is something to replay

BASE="http://localhost${HTTP_ADDR}"
RECEIVER="localhost:19097"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh
trap stop_all EXIT

go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/receiver ./cmd/receiver

start api ./bin/api
start worker ./bin/worker
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api.log"; exit 1; }

# deliveries_of EVENT_ID  -> loads the event's deliveries into BODY
deliveries_of() { call GET "/v1/events/$1/deliveries?limit=100" "$KEY"; }

echo "== setup: endpoints that behave differently, each subscribed to its own event type"
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Replay Co"}'
KEY=$(field 'd["api_key"]["key"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Someone Else"}'
OTHER_KEY=$(field 'd["api_key"]["key"]')

SECRETS=""
add_endpoint() { # add_endpoint NAME PATH EVENT_TYPE -> sets EP_<NAME>
  create_endpoint "$KEY" "http://$RECEIVER$2" "[\"$3\"]"
  printf -v "EP_$1" '%s' "$ENDPOINT_ID"
  SECRETS="${SECRETS:+$SECRETS,}$ENDPOINT_SECRET"
}
add_endpoint FLAKY /flaky/1 single.test
add_endpoint OK /ok ok.test
add_endpoint SLOW "/slow?ms=3000" slow.test
add_endpoint BULK /flaky/1 bulk.test
start receiver ./bin/receiver -addr ":19097" -secret "$SECRETS"
wait_for_http "http://$RECEIVER/_received" || { echo "receiver did not start"; exit 1; }

echo "== replaying a failed delivery"
send_event single.test
EVT1=$EVENT_ID
poll 20 delivery_done "$EVT1" "$EP_FLAKY" || echo "  (timed out waiting)"
ORIG=$(delivery_to "$EVT1" "$EP_FLAKY")
call GET "/v1/deliveries/$ORIG" "$KEY"
expect "the first attempt failed" failed "$(field 'd["status"]')"
expect "with the endpoint's 500" "[500]" "$(field '[a["status_code"] for a in d["attempts"]]')"
expect "and is not itself a replay" None "$(field 'd["replayed_from"]')"

call POST "/v1/deliveries/$ORIG/replay" "$KEY"
expect "replay accepted" 202 "$CODE"
REPLAY=$(field 'd["id"]')
expect "it is a new delivery" True "$(field "d['id'] != '$ORIG'")"
expect "linked to the original" "$ORIG" "$(field 'd["replayed_from"]')"
expect "for the same event" "$EVT1" "$(field 'd["event_id"]')"
expect "to the same endpoint" "$EP_FLAKY" "$(field 'd["endpoint_id"]')"
expect "and starts pending" pending "$(field 'd["status"]')"

replay_done() { call GET "/v1/deliveries/$REPLAY" "$KEY"; [[ $(field 'd["status"]') != "pending" ]]; }
poll 20 replay_done || echo "  (timed out waiting)"
expect "the replay succeeded" succeeded "$(field 'd["status"]')"
expect "on its first attempt" "[200]" "$(field '[a["status_code"] for a in d["attempts"]]')"
call GET "/v1/deliveries/$ORIG" "$KEY"
expect "the original is untouched: still failed" failed "$(field 'd["status"]')"
expect "with its single attempt" 1 "$(field 'len(d["attempts"])')"
deliveries_of "$EVT1"
expect "the event now shows both deliveries" 2 "$(field 'len(d["data"])')"
BODY=$(curl -s "http://$RECEIVER/_received")
expect "the receiver saw the event twice, with the same webhook-id" 2 "$(field "len([r for r in d if r['webhook_id'] == '$EVT1' and r['verified']])")"

echo "== replaying a delivery that succeeded"
send_event ok.test
EVT_OK=$EVENT_ID
poll 20 delivery_done "$EVT_OK" "$EP_OK" || echo "  (timed out waiting)"
OK_DELIVERY=$(delivery_to "$EVT_OK" "$EP_OK")
call POST "/v1/deliveries/$OK_DELIVERY/replay" "$KEY"
expect "a successful delivery can be replayed too" 202 "$CODE"

echo "== what cannot be replayed"
send_event slow.test
EVT_SLOW=$EVENT_ID
call POST "/v1/deliveries/$(delivery_to "$EVT_SLOW" "$EP_SLOW")/replay" "$KEY"
expect "a delivery that is still in flight" 409 "$CODE"
expect "says it is pending" True "$(field '"pending" in d["error"]["message"]')"
call POST "/v1/endpoints/$EP_OK/disable" "$KEY" >/dev/null
call POST "/v1/deliveries/$OK_DELIVERY/replay" "$KEY"
expect "a delivery to a disabled endpoint" 409 "$CODE"
expect "says to enable it first" True "$(field '"disabled" in d["error"]["message"]')"
call POST "/v1/endpoints/$EP_OK/enable" "$KEY" >/dev/null
call POST "/v1/deliveries/$OK_DELIVERY/replay" "$KEY"
expect "after re-enabling it works again" 202 "$CODE"
call POST "/v1/deliveries/$(uuidgen | tr A-Z a-z)/replay" "$KEY"
expect "an unknown delivery" 404 "$CODE"
call POST "/v1/deliveries/$ORIG/replay" "$OTHER_KEY"
expect "another tenant's delivery" 404 "$CODE"
call POST "/v1/deliveries/not-a-uuid/replay" "$KEY"
expect "a malformed id" 400 "$CODE"
call POST "/v1/deliveries/$ORIG/replay" ""
expect "no api key" 401 "$CODE"

echo "== bulk replay of an endpoint's failed deliveries"
BULK_EVENTS=()
for _ in 1 2 3; do
  send_event bulk.test
  BULK_EVENTS+=("$EVENT_ID")
done
all_bulk_done() { for e in "${BULK_EVENTS[@]}"; do delivery_done "$e" "$EP_BULK" || return 1; done; }
poll 30 all_bulk_done || echo "  (timed out waiting)"

SINCE=$(iso_ago 1)
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$SINCE\"}"
expect "bulk replay accepted" 202 "$CODE"
expect "all three failed deliveries were replayed" 3 "$(field 'd["replayed"]')"
expect "nothing is left over" False "$(field 'd["has_more"]')"

all_replays_done() { for e in "${BULK_EVENTS[@]}"; do deliveries_settled "$KEY" "$e" 2 || return 1; done; }
poll 30 all_replays_done || echo "  (timed out waiting)"
SUCCEEDED=0
for e in "${BULK_EVENTS[@]}"; do
  deliveries_of "$e"
  [[ $(field 'sorted(x["status"] for x in d["data"])') == "['failed', 'succeeded']" ]] && SUCCEEDED=$((SUCCEEDED + 1))
done
expect "each event ends with its failed original and a succeeded replay" 3 "$SUCCEEDED"

call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$SINCE\"}"
expect "asking again replays nothing: deliveries that were replayed are skipped" 0 "$(field 'd["replayed"]')"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$(iso_ago 3)\",\"until\":\"$(iso_ago 2)\"}"
expect "a range with no deliveries replays nothing" 0 "$(field 'd["replayed"]')"

echo "== two bulk replays at the same moment must not replay anything twice"
RACE_EVENTS=()
for _ in 1 2 3; do
  send_event bulk.test
  RACE_EVENTS+=("$EVENT_ID")
done
all_race_done() { for e in "${RACE_EVENTS[@]}"; do delivery_done "$e" "$EP_BULK" || return 1; done; }
poll 30 all_race_done || echo "  (timed out waiting)"

bulk_replay_into() { # bulk_replay_into FILE  -> runs a bulk replay and writes the response to FILE
  curl -s -X POST "$BASE/v1/endpoints/$EP_BULK/replay" -H "Authorization: Bearer $KEY" -H "$JSON" \
    -d "{\"since\":\"$SINCE\"}" >"$1"
}
R1=$(mktemp)
R2=$(mktemp)
bulk_replay_into "$R1" &
P1=$!
bulk_replay_into "$R2" &
P2=$!
# Wait for these two only: a bare `wait` would also wait for the API, worker and receiver.
wait "$P1" "$P2"
TOTAL=$(python3 -c "import json,sys; print(sum(json.load(open(f))['replayed'] for f in sys.argv[1:]))" "$R1" "$R2")
rm -f "$R1" "$R2"
expect "between them the two calls replayed each of the 3 failed deliveries exactly once" 3 "$TOTAL"

all_race_replays_done() { for e in "${RACE_EVENTS[@]}"; do deliveries_settled "$KEY" "$e" 2 || return 1; done; }
poll 30 all_race_replays_done || echo "  (timed out waiting)"
DUPLICATED=0
for e in "${RACE_EVENTS[@]}"; do
  deliveries_of "$e"
  [[ $(field 'len(d["data"])') == "2" ]] || DUPLICATED=$((DUPLICATED + 1))
done
expect "no event ended up with a duplicate replay" 0 "$DUPLICATED"

echo "== bulk replay: validation and rules"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" '{}';                                       expect "since is required" 400 "$CODE"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$(iso_ago -1)\"}";           expect "since in the future" 400 "$CODE"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$SINCE\",\"until\":\"$(iso_ago 2)\"}"; expect "until before since" 400 "$CODE"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" '{"since":"yesterday"}';                   expect "a malformed date" 400 "$CODE"
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$SINCE\",\"extra\":1}";      expect "an unknown field" 400 "$CODE"
call POST "/v1/endpoints/$(uuidgen | tr A-Z a-z)/replay" "$KEY" "{\"since\":\"$SINCE\"}";   expect "an unknown endpoint" 404 "$CODE"
call POST "/v1/endpoints/$EP_BULK/replay" "$OTHER_KEY" "{\"since\":\"$SINCE\"}";            expect "another tenant's endpoint" 404 "$CODE"
call POST "/v1/endpoints/$EP_BULK/disable" "$KEY" >/dev/null
call POST "/v1/endpoints/$EP_BULK/replay" "$KEY" "{\"since\":\"$SINCE\"}";                  expect "a disabled endpoint" 409 "$CODE"

finish || { echo; echo "--- worker log (tail)"; tail -15 "$LOG_DIR/worker.log"; echo "--- api log (tail)"; tail -10 "$LOG_DIR/api.log"; exit 1; }
