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

finish() {
  echo
  echo "$PASS passed, $FAIL failed"
  rm -f "$HDR_FILE"
  [[ $FAIL -eq 0 ]]
}
