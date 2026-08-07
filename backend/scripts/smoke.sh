#!/usr/bin/env bash
# End-to-end smoke test of the HTTP API against the real Postgres and Redis.
#
#   make up && make migrate-up && make smoke
#
# It builds and starts the API itself (with SSRF protection ON, unlike the dev default),
# runs the checks below, then stops the server. Exits non-zero if any check fails.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a
export ALLOW_LOCAL_ENDPOINTS=false # exercise the production URL policy
export HTTP_ADDR=":18080"          # avoid clashing with a dev server on :8080

BASE="http://localhost${HTTP_ADDR}"
# shellcheck source=scripts/lib.sh
source scripts/lib.sh

go build -o bin/api ./cmd/api
./bin/api >/dev/null 2>&1 &
API_PID=$!
trap 'kill "$API_PID" 2>/dev/null || true; rm -f "$HDR_FILE"' EXIT

wait_for_http "$BASE/healthz" || { echo "API did not start"; exit 1; }


echo "== tenants and API keys"
call POST /admin/tenants "" '{"name":"Acme"}'
expect "admin route without token" 401 "$CODE"
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Acme"}'
expect "create tenant" 201 "$CODE"
KEY=$(field 'd["api_key"]["key"]')
KEY_ID=$(field 'd["api_key"]["id"]')
TENANT=$(field 'd["tenant"]["id"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":"Other"}'
OTHER_KEY=$(field 'd["api_key"]["key"]')
call POST /admin/tenants "$ADMIN_TOKEN" '{"name":" "}'
expect "blank tenant name" 400 "$CODE"
call GET /v1/tenant ""
expect "no api key" 401 "$CODE"
call GET /v1/tenant "tern_bogus"
expect "bad api key" 401 "$CODE"
call GET /v1/tenant "$ADMIN_TOKEN"
expect "admin token is not an api key" 401 "$CODE"
call GET /v1/tenant "$KEY"
expect "valid api key" 200 "$CODE"
expect "tenant id returned" "$TENANT" "$(field 'd["id"]')"
call POST /v1/api-keys "$KEY" '{"name":"ci"}'
expect "create second key" 201 "$CODE"
KEY2=$(field 'd["key"]')
call GET /v1/api-keys "$KEY"
expect "list keys" 200 "$CODE"
expect "two keys listed" 2 "$(field 'len(d["data"])')"
expect "listing never contains plaintext keys" 0 "$(count '"key"')"
call DELETE "/v1/api-keys/$KEY_ID" "$OTHER_KEY"
expect "cannot revoke another tenant's key" 404 "$CODE"
call DELETE "/v1/api-keys/$KEY_ID" "$KEY2"
expect "revoke key" 204 "$CODE"
call GET /v1/tenant "$KEY"
expect "revoked key rejected" 401 "$CODE"
call DELETE "/v1/api-keys/$KEY_ID" "$KEY2"
expect "revoke is idempotent" 204 "$CODE"
KEY="$KEY2"

echo "== endpoints: create"
call POST /v1/endpoints "" '{"url":"https://example.com/hook"}'
expect "no api key" 401 "$CODE"
call POST /v1/endpoints "$KEY" '{"url":"https://example.com/hook","description":"prod","event_types":["invoice.paid","user.created","invoice.paid"]}'
expect "create endpoint" 201 "$CODE"
EP=$(field 'd["id"]')
SECRET=$(field 'd["secret"]')
expect "secret has whsec_ prefix" True "$(field 'd["secret"].startswith("whsec_")')"
expect "starts active" active "$(field 'd["status"]')"
expect "event types de-duplicated" "['invoice.paid', 'user.created']" "$(field 'd["event_types"]')"
expect "timestamps are UTC" Z "$(field 'd["created_at"][-1]')"

echo "== endpoints: validation and SSRF protection"
reject() { # reject DESCRIPTION JSON_BODY
  call POST /v1/endpoints "$KEY" "$2"
  expect "$1" 400 "$CODE"
}
reject "http rejected" '{"url":"http://example.com/hook"}'
reject "localhost rejected" '{"url":"https://localhost/hook"}'
reject "loopback ip rejected" '{"url":"https://127.0.0.1/hook"}'
reject "private ip rejected" '{"url":"https://10.0.0.5/hook"}'
reject "cloud metadata ip rejected" '{"url":"https://169.254.169.254/latest"}'
reject "ipv6 loopback rejected" '{"url":"https://[::1]/hook"}'
reject "credentials in url rejected" '{"url":"https://u:p@example.com/hook"}'
reject "empty url rejected" '{"url":""}'
reject "bad event type rejected" '{"url":"https://example.com","event_types":["Not Valid"]}'
reject "unknown field rejected" '{"url":"https://example.com","nope":1}'

echo "== endpoints: read"
call GET "/v1/endpoints/$EP" "$KEY"
expect "get endpoint" 200 "$CODE"
expect "get never returns the secret" 0 "$(count '"secret"')"
call GET "/v1/endpoints/not-a-uuid" "$KEY"
expect "bad id" 400 "$CODE"
call GET "/v1/endpoints/$EP" "$OTHER_KEY"
expect "another tenant cannot read it" 404 "$CODE"

echo "== endpoints: pagination"
call POST /v1/endpoints "$KEY" '{"url":"https://example.com/2"}'
call POST /v1/endpoints "$KEY" '{"url":"https://example.com/3"}'
call GET "/v1/endpoints?limit=2" "$KEY"
expect "first page" 200 "$CODE"
expect "page has 2 items" 2 "$(field 'len(d["data"])')"
expect "next cursor present" True "$(field 'd["next_cursor"] is not None')"
expect "list never returns secrets" 0 "$(count '"secret"')"
CURSOR=$(field 'd["next_cursor"]')
call GET "/v1/endpoints?limit=2&cursor=$CURSOR" "$KEY"
expect "second page has the remaining item" 1 "$(field 'len(d["data"])')"
expect "last page has no cursor" None "$(field 'd["next_cursor"]')"
expect "oldest endpoint comes last" "$EP" "$(field 'd["data"][-1]["id"]')"
call GET "/v1/endpoints?limit=0" "$KEY"
expect "limit 0 rejected" 400 "$CODE"
call GET "/v1/endpoints?cursor=bad" "$KEY"
expect "bad cursor rejected" 400 "$CODE"
call GET "/v1/endpoints" "$OTHER_KEY"
expect "other tenant sees none of them" 0 "$(field 'len(d["data"])')"

echo "== endpoints: update"
call PATCH "/v1/endpoints/$EP" "$KEY" '{"description":"staging"}'
expect "patch description" 200 "$CODE"
expect "description changed" staging "$(field 'd["description"]')"
expect "url untouched" "https://example.com/hook" "$(field 'd["url"]')"
call PATCH "/v1/endpoints/$EP" "$KEY" '{"url":"https://10.0.0.1/x"}'
expect "patch to private url rejected" 400 "$CODE"
call GET "/v1/endpoints/$EP" "$KEY"
expect "rejected patch changed nothing" "https://example.com/hook" "$(field 'd["url"]')"
call PATCH "/v1/endpoints/$EP" "$KEY" '{"event_types":[]}'
expect "clear event types" 200 "$CODE"
expect "event types cleared" "[]" "$(field 'd["event_types"]')"
call PATCH "/v1/endpoints/$EP" "$OTHER_KEY" '{"description":"x"}'
expect "another tenant cannot patch" 404 "$CODE"

echo "== endpoints: secret rotation, disable, enable"
call POST "/v1/endpoints/$EP/rotate-secret" "$KEY"
expect "rotate secret" 200 "$CODE"
expect "secret changed" True "$(field "d['secret'] != '$SECRET'")"
call POST "/v1/endpoints/$EP/disable" "$KEY"
expect "disable" 200 "$CODE"
expect "status disabled" disabled "$(field 'd["status"]')"
expect "reason recorded" "disabled manually" "$(field 'd["disabled_reason"]')"
call POST "/v1/endpoints/$EP/enable" "$KEY"
expect "enable" 200 "$CODE"
expect "status active" active "$(field 'd["status"]')"
expect "reason cleared" None "$(field 'd["disabled_reason"]')"
call POST "/v1/endpoints/$EP/disable" "$OTHER_KEY"
expect "another tenant cannot disable" 404 "$CODE"

echo "== endpoints: delete"
call DELETE "/v1/endpoints/$EP" "$OTHER_KEY"
expect "another tenant cannot delete" 404 "$CODE"
call DELETE "/v1/endpoints/$EP" "$KEY"
expect "delete" 204 "$CODE"
call GET "/v1/endpoints/$EP" "$KEY"
expect "gone after delete" 404 "$CODE"
call DELETE "/v1/endpoints/$EP" "$KEY"
expect "second delete" 404 "$CODE"

finish
