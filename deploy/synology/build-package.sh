#!/bin/sh
set -eu

repository_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
output_dir="${1:-${repository_root}/runtime/synology-package}"
version="${VERSION:-e1-local}"
image="family-dashboard:e1-amd64"

mkdir -p "$output_dir"

if docker buildx version >/dev/null 2>&1; then
  buildx="docker buildx"
elif [ -x /opt/homebrew/lib/docker/cli-plugins/docker-buildx ]; then
  buildx="/opt/homebrew/lib/docker/cli-plugins/docker-buildx"
else
  echo "Docker Buildx is required." >&2
  exit 1
fi

# Intentional word splitting supports both "docker buildx" and a direct plugin path.
# shellcheck disable=SC2086
$buildx build \
  --file "${repository_root}/deploy/synology/Dockerfile" \
  --platform linux/amd64 \
  --build-arg "VERSION=${version}" \
  --load \
  --tag "$image" \
  "$repository_root"

docker image inspect "$image" --format '{{.Architecture}} {{.Os}} {{.Size}}'
docker save "$image" -o "${output_dir}/family-dashboard-e1-amd64.tar"

cp "${repository_root}/deploy/synology/compose.yaml" "${output_dir}/compose.yaml"
cp "${repository_root}/deploy/synology/e1-verify.sh" "${output_dir}/e1-verify.sh"
cp "${repository_root}/deploy/synology/e1-monitor.sh" "${output_dir}/e1-monitor.sh"

echo "Synology package created at ${output_dir}"
