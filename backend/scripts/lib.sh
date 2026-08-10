# Shared helpers for the smoke scripts. Source this file; do not execute it.
#
# A script using it should have `set -euo pipefail`, define BASE (the API's URL), and call
# `finish` as its last line. After `call`, the response is in CODE, BODY and (via `header`) HEADERS.

JSON='Content-Type: application/json'
PASS=0
FAIL=0
CODE=""
BODY=""
HDR_FILE=$(mktemp)

# call METHOD PATH TOKEN [JSON_BODY [EXTRA_CURL_ARGS...]]  -> sets CODE and BODY
call() {
  local method=$1 path=$2 token=$3 data=${4:-}
  shift $(($# < 4 ? $# : 4))
  local args=(-s -X "$method" -D "$HDR_FILE" -w $'\n%{http_code}' "$BASE$path")
  [[ -n $token ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n $data ]] && args+=(-H "$JSON" -d "$data")
  args+=("$@")
  local out
  out=$(curl "${args[@]}")
  CODE=${out##*$'\n'}
  BODY=${out%$'\n'*}
}

# header NAME  -> value of a response header of the last call (empty if absent)
header() { grep -i "^$1:" "$HDR_FILE" | head -1 | cut -d: -f2- | tr -d ' \r' || true; }

# field PYTHON_EXPR  -> evaluates the expression against the last response body as `d`
field() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)" <<<"$BODY"; }

# count PATTERN -> how many lines of the last response body match (0 if none)
count() { grep -c "$1" <<<"$BODY" || true; }

# expect DESCRIPTION WANT GOT
expect() {
  if [[ "$2" == "$3" ]]; then
    PASS=$((PASS + 1))
    echo "ok   $1"
  else
    FAIL=$((FAIL + 1))
    echo "FAIL $1 (want '$2', got '$3')"
  fi
}

# poll TIMEOUT_SECONDS COMMAND [ARGS...]  -> succeeds once the command succeeds within the timeout
poll() {
  local deadline=$((SECONDS + $1))
  shift
  until "$@"; do
    ((SECONDS >= deadline)) && return 1
    sleep 0.3
  done
}

# wait_for_http URL  -> waits up to 10s for a 2xx response
wait_for_http() { poll 10 curl -sf -o /dev/null "$1"; }

# ---- running processes and creating fixtures (used by the end-to-end scripts) ----------------

LOG_DIR=$(mktemp -d)
PIDS=()
LAST_PID=""

# start NAME COMMAND [ARGS...]  -> runs a process in the background, logging to $LOG_DIR/NAME.log;
# its pid is left in LAST_PID
start() {
  local name=$1
  shift
  "$@" >"$LOG_DIR/$name.log" 2>&1 &
  LAST_PID=$!
  PIDS+=("$LAST_PID")
}

# stop_all  -> stops everything start launched and removes the temp files; use it in an EXIT trap
stop_all() {
  for pid in ${PIDS[@]+"${PIDS[@]}"}; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true # reaps it, so the shell does not print "Terminated"
  done
  rm -f "$HDR_FILE"
  rm -rf "$LOG_DIR"
}

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
# (uses $KEY as the API key; prints "none" if there is no such delivery)
delivery_to() {
  call GET "/v1/events/$1/deliveries?limit=100" "$KEY"
  field "next((x['id'] for x in d['data'] if x['endpoint_id'] == '$2'), 'none')"
}

# send_event TYPE  -> sends an event as $KEY and sets EVENT_ID
send_event() {
  call POST /v1/events "$KEY" "{\"type\":\"$1\",\"payload\":{\"n\":1}}"
  EVENT_ID=$(field 'd["id"]')
}

# delivery_done EVENT_ID ENDPOINT_ID  -> true once that delivery is no longer pending (uses $KEY)
delivery_done() {
  call GET "/v1/deliveries/$(delivery_to "$1" "$2")" "$KEY"
  [[ $(field 'd["status"]') != "pending" ]]
}

# iso_ago HOURS  -> an RFC 3339 UTC timestamp that many hours in the past
iso_ago() {
  python3 -c "import datetime as d; print((d.datetime.now(d.timezone.utc) - d.timedelta(hours=$1)).strftime('%Y-%m-%dT%H:%M:%SZ'))"
}

finish() {
  echo
  echo "$PASS passed, $FAIL failed"
  rm -f "$HDR_FILE"
  [[ $FAIL -eq 0 ]]
}
