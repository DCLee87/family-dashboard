#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this script with sudo." >&2
  exit 1
fi

docker_bin="/usr/local/bin/docker"
deployment_dir="/volume1/docker/family-dashboard"
package_dir="${1:-/volume1/homes/maius0513/family-dashboard-c4}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="${deployment_dir}/backups/pre-c4-${timestamp}"

test -x "$docker_bin"
test -d "$deployment_dir"
test -f "${package_dir}/family-dashboard-c4-amd64.tar"
test -f "${package_dir}/compose.yaml"

cd "$deployment_dir"
mkdir -p "$backup_dir"
"$docker_bin" compose stop

for configuration_file in compose.yaml .env push.env
do
  if [ -f "$configuration_file" ]; then
    cp -p "$configuration_file" "$backup_dir/"
  fi
done
for database_file in data/family-dashboard.db data/family-dashboard.db-wal data/family-dashboard.db-shm
do
  if [ -f "$database_file" ]; then
    cp -p "$database_file" "$backup_dir/"
  fi
done

"$docker_bin" load -i "${package_dir}/family-dashboard-c4-amd64.tar"
cp "${package_dir}/compose.yaml" compose.yaml
test -f push.env
chmod 600 push.env

network_cidr="$(
  "$docker_bin" network inspect family-dashboard_default \
    --format '{{(index .IPAM.Config 0).Subnet}}'
)"
case "$network_cidr" in
  */*) ;;
  *) echo "Could not determine the Compose network CIDR." >&2; exit 1 ;;
esac

umask 077
printf 'FAMILY_DASHBOARD_LOCAL_NETWORKS=%s\n' "$network_cidr" > .env
printf 'FAMILY_DASHBOARD_SECURE_COOKIES=true\n' >> .env

if ! "$docker_bin" compose run --rm family-dashboard --check-db; then
  echo "C4 database check failed. The service remains stopped." >&2
  echo "Restore with: sudo sh c4-rollback.sh ${backup_dir}" >&2
  exit 1
fi

"$docker_bin" compose up -d
echo "Backup created at: ${backup_dir}"
echo "C4 deployment started. Verify health, task notifications, history, skips and Push."
