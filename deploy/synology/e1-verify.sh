#!/bin/sh
set -eu

base_url="${FAMILY_DASHBOARD_URL:-http://127.0.0.1:8080}"
compose_command="${COMPOSE_COMMAND:-docker compose}"

echo "health endpoint"
curl --fail --silent --show-error "${base_url}/api/health"
echo

echo "runtime endpoint"
curl --fail --silent --show-error "${base_url}/api/runtime"
echo

echo "container state"
# Intentional word splitting lets COMPOSE_COMMAND contain "docker compose".
# shellcheck disable=SC2086
$compose_command ps

echo "database integrity"
# shellcheck disable=SC2086
$compose_command exec -T family-dashboard /family-dashboard --check-db
