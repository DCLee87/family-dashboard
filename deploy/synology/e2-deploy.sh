#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this script with sudo." >&2
  exit 1
fi

docker_bin="/usr/local/bin/docker"
deployment_dir="/volume1/docker/family-dashboard"
package_dir="${1:-/volume1/homes/maius0513/family-dashboard-e2}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="${deployment_dir}/backups/pre-e2-${timestamp}"

test -x "$docker_bin"
test -f "${package_dir}/family-dashboard-e2-amd64.tar"
test -f "${package_dir}/compose.yaml"

cd "$deployment_dir"
mkdir -p "$backup_dir"

"$docker_bin" compose stop

cp -p compose.yaml "${backup_dir}/compose.yaml"
if [ -f push.env ]; then
  cp -p push.env "${backup_dir}/push.env"
fi
for database_file in \
  data/family-dashboard.db \
  data/family-dashboard.db-wal \
  data/family-dashboard.db-shm
do
  if [ -f "$database_file" ]; then
    cp -p "$database_file" "$backup_dir/"
  fi
done

"$docker_bin" load -i "${package_dir}/family-dashboard-e2-amd64.tar"
cp "${package_dir}/compose.yaml" compose.yaml

if [ ! -f push.env ]; then
  umask 077
  "$docker_bin" run --rm family-dashboard:e2-amd64 \
    --generate-vapid-keys > push.env
fi
chmod 600 push.env

network_cidr="$(
  "$docker_bin" network inspect family-dashboard_default \
    --format '{{(index .IPAM.Config 0).Subnet}}'
)"
case "$network_cidr" in
  */*) ;;
  *)
    echo "Could not determine the Compose network CIDR." >&2
    exit 1
    ;;
esac

umask 077
printf 'FAMILY_DASHBOARD_LOCAL_NETWORKS=%s\n' "$network_cidr" > .env
printf 'FAMILY_DASHBOARD_SECURE_COOKIES=true\n' >> .env

"$docker_bin" compose up -d

echo "Backup created at: ${backup_dir}"
echo "E2 deployment started. Check container logs for the one-time setup code."
