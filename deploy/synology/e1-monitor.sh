#!/bin/sh
set -eu

container="${FAMILY_DASHBOARD_CONTAINER:-family-dashboard-family-dashboard-1}"
interval="${MONITOR_INTERVAL_SECONDS:-60}"
duration="${MONITOR_DURATION_SECONDS:-86400}"
output="${MONITOR_OUTPUT:-e1-monitor.csv}"
docker="${DOCKER_COMMAND:-/usr/local/bin/docker}"
started_at="$(date +%s)"

if [ ! -f "$output" ]; then
  echo "timestamp,cpu_percent,memory_usage,net_io,block_io,pids" > "$output"
fi

while :; do
  timestamp="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  metrics="$("$docker" stats --no-stream --format '{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}},{{.PIDs}}' "$container")"
  echo "${timestamp},${metrics}" >> "$output"
  now="$(date +%s)"
  if [ $((now - started_at)) -ge "$duration" ]; then
    break
  fi
  sleep "$interval"
done
