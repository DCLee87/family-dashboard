#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then echo "Run this script with sudo." >&2; exit 1; fi
if [ "$#" -ne 1 ]; then echo "Usage: sudo sh c5-rollback.sh /volume1/docker/family-dashboard/backups/pre-c5-<UTC>" >&2; exit 1; fi
docker_bin="/usr/local/bin/docker"; deployment_dir="/volume1/docker/family-dashboard"; backup_dir="$1"
case "$backup_dir" in "${deployment_dir}/backups/pre-c5-"*) ;; *) echo "Refusing backup path outside the C5 backup directory." >&2; exit 1 ;; esac
test -x "$docker_bin"; test -f "${backup_dir}/compose.yaml"; test -f "${backup_dir}/family-dashboard.db"
cd "$deployment_dir"; "$docker_bin" compose stop; cp -p "${backup_dir}/compose.yaml" compose.yaml
for configuration_file in .env push.env; do if [ -f "${backup_dir}/${configuration_file}" ]; then cp -p "${backup_dir}/${configuration_file}" "$configuration_file"; fi; done
rm -f data/family-dashboard.db data/family-dashboard.db-wal data/family-dashboard.db-shm
cp -p "${backup_dir}/family-dashboard.db" data/family-dashboard.db
for database_file in family-dashboard.db-wal family-dashboard.db-shm; do if [ -f "${backup_dir}/${database_file}" ]; then cp -p "${backup_dir}/${database_file}" "data/${database_file}"; fi; done
"$docker_bin" compose up -d
echo "Rollback restored from: ${backup_dir}"
