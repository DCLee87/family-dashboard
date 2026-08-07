#!/bin/sh
set -eu

repository_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
output_dir="${1:-${repository_root}/runtime/synology-package-c2}"
image="family-dashboard:c2-amd64"
archive="${output_dir}/family-dashboard-c2-amd64.tar"

mkdir -p "$output_dir"

if docker buildx version >/dev/null 2>&1; then
  set -- docker buildx
elif command -v docker-buildx >/dev/null 2>&1; then
  set -- docker-buildx
else
  echo "Docker Buildx is required to create the linux/amd64 package." >&2
  exit 1
fi

"$@" build \
  --file "${repository_root}/deploy/synology/Dockerfile" \
  --platform linux/amd64 \
  --build-arg VERSION=c2 \
  --tag "$image" \
  --load \
  "$repository_root"

docker save "$image" -o "$archive"
cp "${repository_root}/deploy/synology/c2-compose.yaml" "${output_dir}/compose.yaml"
cp "${repository_root}/deploy/synology/c2-deploy.sh" "${output_dir}/c2-deploy.sh"
cp "${repository_root}/deploy/synology/c2-rollback.sh" "${output_dir}/c2-rollback.sh"
cp "${repository_root}/deploy/synology/e1-verify.sh" "${output_dir}/e1-verify.sh"
(
  cd "$output_dir"
  shasum -a 256 "$(basename "$archive")" > "$(basename "$archive").sha256"
)

echo "C2 Synology package created at ${output_dir}"
