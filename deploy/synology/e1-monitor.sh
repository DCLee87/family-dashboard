#!/bin/sh
set -eu

container="${FAMILY_DASHBOARD_CONTAINER:-family-dashboard-family-dashboard-1}"
interval="${MONITOR_INTERVAL_SECONDS:-60}"
output="${MONITOR_OUTPUT:-e1-monitor.csv}"

if [ ! -f "$output" ]; then
  echo "timestamp,cpu_percent,memory_usage,net_io,block_io,pids" > "$output"
fi

while :; do
  timestamp="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  metrics="$(docker stats --no-stream --format '{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}},{{.PIDs}}' "$container")"
  echo "${timestamp},${metrics}" >> "$output"
  sleep "$interval"
done
