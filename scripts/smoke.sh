#!/bin/sh
set -eu

base_url="${BASE_URL:-http://localhost:8080}"

curl --fail --silent --show-error \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"operation_id":"release-92","flag_key":"devtools.symbol-index","percentage":10}' \
  "$base_url/release-operations"
printf '\n'

curl --fail --silent --show-error \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"build_id":"build-1842","commit":"a13f24c","status":"passed"}' \
  "$base_url/build-events"
printf '\n'

curl --fail --silent --show-error \
  --request GET \
  "$base_url/diagnostics"
printf '\n'
