#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${BUNDLE_OUTPUT:-${root}/deployment-bundle}"
image_ref="${IMAGE_REF:?IMAGE_REF is required}"
image_id="${IMAGE_ID:?IMAGE_ID is required}"
mode="${DEPLOYMENT_MODE:-normal}"
compose_source="${COMPOSE_SOURCE:-${root}/docker-compose.yml}"

[[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'invalid image id\n' >&2; exit 1; }
[[ "${mode}" == normal || "${mode}" == recovery || "${mode}" == legacy-migration ]] || { printf 'invalid deployment mode\n' >&2; exit 1; }
[[ ! -e "${output}" ]] || { printf 'bundle output already exists\n' >&2; exit 1; }
mkdir -m 0700 "${output}"

install -m 0600 "${compose_source}" "${output}/docker-compose.yml"
install -m 0600 "${root}/deploy/kitsusync-deploy" "${output}/kitsusync-deploy"
install -m 0600 "${root}/deploy/kitsusync-inspect" "${output}/kitsusync-inspect"
install -m 0600 "${root}/deploy/kitsusync-sqlite-backup" "${output}/kitsusync-sqlite-backup"
install -m 0600 "${root}/deploy/kitsusync-bootstrap" "${output}/kitsusync-bootstrap"
printf '%s\n' "${mode}" >"${output}/deployment-mode"
chmod 0600 "${output}/deployment-mode"

docker save --output "${output}/kitsusync-image.tar" "${image_ref}"
chmod 0600 "${output}/kitsusync-image.tar"
archive_sha="$(sha256sum "${output}/kitsusync-image.tar" | cut -d' ' -f1)"
compose_sha="$(sha256sum "${output}/docker-compose.yml" | cut -d' ' -f1)"
deploy_sha="$(sha256sum "${output}/kitsusync-deploy" | cut -d' ' -f1)"
inspect_sha="$(sha256sum "${output}/kitsusync-inspect" | cut -d' ' -f1)"
backup_sha="$(sha256sum "${output}/kitsusync-sqlite-backup" | cut -d' ' -f1)"
bootstrap_sha="$(sha256sum "${output}/kitsusync-bootstrap" | cut -d' ' -f1)"

IMAGE_ARCHIVE_SHA256="${archive_sha}" COMPOSE_SHA256="${compose_sha}" \
DEPLOYMENT_TOOL_SHA256="${deploy_sha}" INSPECTION_TOOL_SHA256="${inspect_sha}" \
SQLITE_BACKUP_TOOL_SHA256="${backup_sha}" BOOTSTRAP_TOOL_SHA256="${bootstrap_sha}" PROVENANCE_OUTPUT="${output}/provenance.txt" \
bash "${root}/scripts/generate-provenance.sh"
chmod 0600 "${output}/provenance.txt"

# Exercise the deployable artifact, not only the in-daemon build result.
docker image rm "${image_ref}" >/dev/null
docker load --input "${output}/kitsusync-image.tar" >/dev/null
loaded_id="$(docker image inspect --format '{{.Id}}' "${image_ref}")"
[[ "${loaded_id}" == "${image_id}" ]] || { printf 'loaded image identity mismatch\n' >&2; exit 1; }
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "${image_ref}")" == "${SOURCE_COMMIT}" ]] || { printf 'loaded image revision mismatch\n' >&2; exit 1; }
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source-id"}}' "${image_ref}")" == "${SOURCE_ID}" ]] || { printf 'loaded image source mismatch\n' >&2; exit 1; }
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "${image_ref}")" == "${RELEASE_VERSION}" ]] || { printf 'loaded image version mismatch\n' >&2; exit 1; }
[[ "$(sha256sum "${output}/kitsusync-image.tar" | cut -d' ' -f1)" == "${archive_sha}" ]] || { printf 'archive digest changed\n' >&2; exit 1; }
[[ "$(sha256sum "${output}/docker-compose.yml" | cut -d' ' -f1)" == "${compose_sha}" ]] || { printf 'Compose digest changed\n' >&2; exit 1; }

printf 'deployment-bundle=verified path=%s image_id=%s archive_sha256=%s\n' "${output}" "${loaded_id}" "${archive_sha}"
