#!/usr/bin/env bash
# End-to-end test of the delivery log, statistics, retention, metrics and the served OpenAPI
# document, with the real API, worker and receiver processes against the real Postgres and Redis:
#
#   make up && make migrate-up && ./scripts/smoke_observability.sh
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=true # endpoints point at the local receiver
export HTTP_ADDR=":18086"         # avoid clashing with a dev server on :8080
export RETRY_SCHEDULE=none        # one attempt per delivery: a failing endpoint stays failed
export DELIVERY_TIMEOUT=2s
export RETENTION_DAYS=30
WORKER_METRICS=":19102"

BASE="http://localhost${HTTP_ADDR}"
RECEIVER="localhost:19095"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh
trap stop_all EXIT

go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/receiver ./cmd/receiver

PG=$(docker compose -f ../docker-compose.yml ps -q postgres)
sql() { docker exec "$PG" psql -U tern -d tern -tA -c "$1"; }

start api ./bin/api
wait_for_http "$BASE/healthz" || { echo "API did not start"; cat "$LOG_DIR/api.log"; exit 1; }

echo "== setup: two tenants, a working endpoint and a failing one"
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Observed Co"}'
KEY=$(field 'd["api_key"]["key"]')
call GET /v1/tenant "$KEY"
TENANT=$(field 'd["id"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Quiet Co"}'
OTHER_KEY=$(field 'd["api_key"]["key"]')

create_endpoint "$KEY" "http://$RECEIVER/ok" '[]'
EP_OK=$ENDPOINT_ID
SECRETS="$ENDPOINT_SECRET"
create_endpoint "$KEY" "http://$RECEIVER/fail" '[]'
EP_FAIL=$ENDPOINT_ID
SECRETS="$SECRETS,$ENDPOINT_SECRET"
start receiver ./bin/receiver -addr ":19095" -secret "$SECRETS"
wait_for_http "http://$RECEIVER/_received" || { echo "receiver did not start"; exit 1; }

# Retention: one event 40 days old (past the 30-day retention) with a delivery and an attempt, and
# one a day old (kept). They are inserted before the worker starts, because it purges on startup.
OLD_EVENT=$(uuidgen | tr A-Z a-z)
OLD_DELIVERY=$(uuidgen | tr A-Z a-z)
RECENT_EVENT=$(uuidgen | tr A-Z a-z)
sql "
  INSERT INTO events (id, tenant_id, type, payload, created_at) VALUES
    ('$OLD_EVENT', '$TENANT', 'old.event', '{}', now() - interval '40 days'),
    ('$RECENT_EVENT', '$TENANT', 'recent.event', '{}', now() - interval '1 day');
  INSERT INTO deliveries (id, tenant_id, event_id, endpoint_id, status, attempt_count, created_at, updated_at, completed_at)
    VALUES ('$OLD_DELIVERY', '$TENANT', '$OLD_EVENT', '$EP_OK', 'succeeded', 1,
            now() - interval '40 days', now() - interval '40 days', now() - interval '40 days');
  INSERT INTO delivery_attempts (id, delivery_id, attempt_number, status_code, duration_ms, started_at)
    VALUES (gen_random_uuid(), '$OLD_DELIVERY', 1, 200, 10, now() - interval '40 days');" >/dev/null

start worker env WORKER_METRICS_ADDR="$WORKER_METRICS" ./bin/worker
WORKER_PID=$LAST_PID
wait_for_http "http://localhost$WORKER_METRICS/healthz" || { echo "worker metrics did not start"; cat "$LOG_DIR/worker.log"; exit 1; }

echo "== retention"
old_gone() { [[ $(sql "SELECT count(*) FROM events WHERE id = '$OLD_EVENT'") == "0" ]]; }
poll 20 old_gone || echo "  (timed out waiting)"
expect "the event past its retention period was deleted" 0 "$(sql "SELECT count(*) FROM events WHERE id = '$OLD_EVENT'")"
expect "with its delivery" 0 "$(sql "SELECT count(*) FROM deliveries WHERE id = '$OLD_DELIVERY'")"
expect "and its attempts" 0 "$(sql "SELECT count(*) FROM delivery_attempts WHERE delivery_id = '$OLD_DELIVERY'")"
expect "a recent event is kept" 1 "$(sql "SELECT count(*) FROM events WHERE id = '$RECENT_EVENT'")"

echo "== send three events: each succeeds at one endpoint and fails at the other"
EVENTS=()
for _ in 1 2 3; do
  send_event obs.test
  EVENTS+=("$EVENT_ID")
done
all_settled() { for e in "${EVENTS[@]}"; do deliveries_settled "$KEY" "$e" 2 || return 1; done; }
poll 30 all_settled || echo "  (timed out waiting)"

echo "== the delivery log and its filters"
count_deliveries() { call GET "/v1/deliveries?limit=100&$1" "$KEY"; field 'len(d["data"])'; }
expect "all deliveries" 6 "$(count_deliveries '')"
expect "only failed ones" 3 "$(count_deliveries 'status=failed')"
expect "only succeeded ones" 3 "$(count_deliveries 'status=succeeded')"
expect "only pending ones" 0 "$(count_deliveries 'status=pending')"
expect "one endpoint" 3 "$(count_deliveries "endpoint_id=$EP_OK")"
expect "one event" 2 "$(count_deliveries "event_id=${EVENTS[0]}")"
expect "an endpoint and a status together" 3 "$(count_deliveries "endpoint_id=$EP_FAIL&status=failed")"
expect "a combination that matches nothing" 0 "$(count_deliveries "endpoint_id=$EP_OK&status=failed")"
expect "created since an hour ago" 6 "$(count_deliveries "since=$(iso_ago 1)")"
expect "created since an hour from now" 0 "$(count_deliveries "since=$(iso_ago -1)")"
expect "created before an hour ago" 0 "$(count_deliveries "until=$(iso_ago 1)")"
expect "created within a window around now" 6 "$(count_deliveries "since=$(iso_ago 1)&until=$(iso_ago -1)")"

call GET "/v1/deliveries?endpoint_id=$EP_OK" "$KEY"
expect "the endpoint filter really selects that endpoint" 3 "$(field "sum(1 for x in d['data'] if x['endpoint_id'] == '$EP_OK')")"
expect "and its deliveries all succeeded" 3 "$(field 'sum(1 for x in d["data"] if x["status"] == "succeeded")')"

call GET "/v1/deliveries?limit=100" "$OTHER_KEY"
expect "another tenant sees none of them" 0 "$(field 'len(d["data"])')"

echo "== pagination of the log"
call GET "/v1/deliveries?limit=4" "$KEY"
expect "first page" 4 "$(field 'len(d["data"])')"
FIRST_PAGE=$(field '[x["id"] for x in d["data"]]')
CURSOR=$(field 'd["next_cursor"]')
call GET "/v1/deliveries?limit=4&cursor=$CURSOR" "$KEY"
expect "second page holds the rest" 2 "$(field 'len(d["data"])')"
expect "and is the last" None "$(field 'd["next_cursor"]')"
expect "no delivery appears on both pages" True "$(field "not (set(x['id'] for x in d['data']) & set($FIRST_PAGE))")"
call GET "/v1/deliveries?limit=100" "$KEY"
expect "newest first" True "$(field '[x["id"] for x in d["data"]] == sorted((x["id"] for x in d["data"]), reverse=True)')"

echo "== filter validation"
call GET "/v1/deliveries?status=exploded" "$KEY";          expect "an unknown status" 400 "$CODE"
call GET "/v1/deliveries?endpoint_id=nope" "$KEY";         expect "a malformed endpoint id" 400 "$CODE"
call GET "/v1/deliveries?event_id=nope" "$KEY";            expect "a malformed event id" 400 "$CODE"
call GET "/v1/deliveries?since=yesterday" "$KEY";          expect "a malformed date" 400 "$CODE"
call GET "/v1/deliveries?since=$(iso_ago -1)&until=$(iso_ago 1)" "$KEY"; expect "since after until" 400 "$CODE"
call GET "/v1/deliveries?limit=0" "$KEY";                  expect "a limit of zero" 400 "$CODE"
call GET "/v1/deliveries" "";                              expect "no api key" 401 "$CODE"
call GET "/v1/events/${EVENTS[0]}/deliveries" "$KEY";      expect "the per-event list still works" 200 "$CODE"
expect "and shows both deliveries of the event" 2 "$(field 'len(d["data"])')"

echo "== statistics"
call GET /v1/stats/deliveries "$KEY"
expect "stats answer" 200 "$CODE"
expect "six deliveries in total" 6 "$(field 'd["deliveries"]["total"]')"
expect "three succeeded" 3 "$(field 'd["deliveries"]["succeeded"]')"
expect "three failed" 3 "$(field 'd["deliveries"]["failed"]')"
expect "none pending" 0 "$(field 'd["deliveries"]["pending"]')"
expect "the success rate is one half" 0.5 "$(field 'd["success_rate"]')"
expect "every attempt got an HTTP response" 6 "$(field 'd["response_time_ms"]["attempts"]')"
expect "response times are plausible" True "$(field '0 <= d["response_time_ms"]["p50"] <= d["response_time_ms"]["p95"] < 2000')"
expect "the default is hourly buckets" hour "$(field 'd["interval"]')"
expect "covering about 24 hours" True "$(field 'len(d["series"]) in (24, 25)')"
expect "the series adds up to the total" 6 "$(field 'sum(x["succeeded"] + x["failed"] + x["pending"] for x in d["series"])')"
expect "every bucket starts on the hour" True "$(field 'all(x["start"][14:16] == "00" for x in d["series"])')"
expect "the whole 24 hours is covered with no gaps" True "$(field '[x["start"] for x in d["series"]] == sorted(x["start"] for x in d["series"])')"

call GET "/v1/stats/deliveries?endpoint_id=$EP_FAIL" "$KEY"
expect "stats for one endpoint" 3 "$(field 'd["deliveries"]["total"]')"
expect "an endpoint that always fails has a success rate of 0" 0 "$(field '"%g" % d["success_rate"]')"
call GET "/v1/stats/deliveries?endpoint_id=$EP_OK" "$KEY"
expect "an endpoint that always works has a success rate of 1" 1 "$(field '"%g" % d["success_rate"]')"

call GET "/v1/stats/deliveries?interval=day&since=$(iso_ago 72)" "$KEY"
expect "daily buckets" day "$(field 'd["interval"]')"
expect "start at midnight UTC" True "$(field 'all(x["start"].endswith("T00:00:00Z") for x in d["series"])')"
expect "and still add up" 6 "$(field 'sum(x["succeeded"] + x["failed"] + x["pending"] for x in d["series"])')"

call GET /v1/stats/deliveries "$OTHER_KEY"
expect "a tenant with no deliveries has none" 0 "$(field 'd["deliveries"]["total"]')"
expect "and no success rate, rather than a misleading 0" None "$(field 'd["success_rate"]')"
expect "but still a full series of empty buckets" True "$(field 'len(d["series"]) >= 24 and all(x["succeeded"] == 0 for x in d["series"])')"

call GET "/v1/stats/deliveries?interval=week" "$KEY";                    expect "an unknown interval" 400 "$CODE"
call GET "/v1/stats/deliveries?since=$(iso_ago 800)" "$KEY";             expect "more than 31 days" 400 "$CODE"
call GET "/v1/stats/deliveries?since=$(iso_ago -1)" "$KEY";              expect "a start in the future" 400 "$CODE"
call GET "/v1/stats/deliveries?endpoint_id=nope" "$KEY";                 expect "a malformed endpoint id" 400 "$CODE"
call GET /v1/stats/deliveries "";                                        expect "no api key" 401 "$CODE"

echo "== metrics: the API"
call GET /wp-login.php ""   # a probe for a URL that does not exist
call GET "/v1/endpoints/$EP_OK" "$KEY"
metrics_page() { curl -s -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/metrics"; }
call GET /metrics "";                       expect "no token" 401 "$CODE"
call GET /metrics "$KEY";                   expect "a tenant API key is not enough" 401 "$CODE"
call GET /metrics "$ADMIN_TOKEN";           expect "the admin token opens it" 200 "$CODE"
PAGE=$(metrics_page)
# has TEXT -> 1 if the page mentions it, else 0 (histograms repeat a name on many lines)
has() { grep -qF -- "$1" <<<"$PAGE" && echo 1 || echo 0; }
expect "counts the events accepted" 1 "$(has 'tern_http_requests_total{method="POST",route="/v1/events",status="202"} 3')"
expect "labels requests by route pattern" 1 "$(has 'route="/v1/endpoints/{endpointID}"')"
expect "never by the concrete URL (no ids as labels)" 0 "$(has "$EP_OK")"
expect "and groups unknown URLs under one label" 1 "$(has 'route="unmatched"')"
expect "without echoing what was probed" 0 "$(has 'wp-login')"
expect "measures latency" 1 "$(has 'tern_http_request_duration_seconds_count{method="POST",route="/v1/events"} 3')"
expect "reports the queue's due jobs" 1 "$(has 'tern_queue_jobs{state="due"}')"
expect "its scheduled jobs" 1 "$(has 'tern_queue_jobs{state="scheduled"}')"
expect "its in-flight jobs" 1 "$(has 'tern_queue_jobs{state="processing"}')"
expect "and its lag" 1 "$(has 'tern_queue_oldest_due_job_age_seconds')"
expect "includes Go runtime metrics" 1 "$(has 'go_goroutines')"

echo "== metrics: the worker (its own port)"
WPAGE=$(curl -s "http://localhost$WORKER_METRICS/metrics")
whas() { grep -qF -- "$1" <<<"$WPAGE" && echo 1 || echo 0; }
expect "counts successful attempts" 1 "$(whas 'tern_delivery_attempts_total{outcome="succeeded"} 3')"
expect "and failed ones" 1 "$(whas 'tern_delivery_attempts_total{outcome="failed"} 3')"
expect "measures how long endpoints took" 1 "$(whas 'tern_delivery_response_seconds_count 6')"
expect "counts the jobs it completed" 1 "$(whas 'tern_worker_jobs_total{result="completed"}')"
expect "and has none running now" 1 "$(whas 'tern_worker_jobs_in_flight 0')"
CODE=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost$WORKER_METRICS/healthz")
expect "the worker answers liveness checks" 200 "$CODE"

echo "== the OpenAPI document"
call GET /openapi.yaml ""
expect "is served without authentication" 200 "$CODE"
expect "as YAML" application/yaml "$(header Content-Type)"
expect "and is OpenAPI 3" 1 "$(grep -c '^openapi: 3' <<<"$BODY" | tr -d ' ')"
expect "describing this API" 1 "$(grep -c 'title: Tern API' <<<"$BODY" | tr -d ' ')"

echo "== graceful shutdown"
kill -TERM "$WORKER_PID"
worker_stopped() { grep -q "worker stopped cleanly" "$LOG_DIR/worker.log"; }
expect "the worker (and its metrics server) shut down cleanly" ok "$(poll 10 worker_stopped && echo ok || echo no)"

finish || { echo; echo "--- worker log (tail)"; tail -15 "$LOG_DIR/worker.log"; echo "--- api log (tail)"; tail -10 "$LOG_DIR/api.log"; exit 1; }
