#!/usr/bin/env bash
set -euo pipefail

# Static contract gate for the root-installed deployment boundary. This test is
# intentionally non-deploying: production inputs, Docker state, and secrets are
# unavailable to CI. It prevents a future edit from weakening the checks that
# the runtime wrapper enforces before it recreates KitsuSync.

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wrapper="$root/deploy/kitsusync-deploy"
dockerfile="$root/Dockerfile"
compose="$root/docker-compose.yml"

require() {
  local pattern="$1" file="$2"
  grep -Fq -- "$pattern" "$file" || {
    printf 'missing release hardening contract: %s (%s)\n' "$pattern" "$file" >&2
    exit 1
  }
}

reject() {
  local pattern="$1" file="$2"
  if grep -Fq -- "$pattern" "$file"; then
    printf 'forbidden password-era deployment contract: %s (%s)\n' "$pattern" "$file" >&2
    exit 1
  fi
}

bash -n "$wrapper"

# F05: an HTTP response alone is not a deployment success. Setup/recovery and
# authenticated admin entry points must remain available or correctly protected.
require 'State.Health' "$wrapper"
require '/api/setup/status' "$wrapper"
require '/bot/setup' "$wrapper"
require '/bot/admin/health' "$wrapper"
require '/ready' "$wrapper"
require 'readiness_allows' "$wrapper"
require 'deployment-mode' "$wrapper"
require '"status"[[:space:]]*:[[:space:]]*"ok"' "$wrapper"

# F06: rollback is tied to an immutable image and the saved compatible runtime
# contract, rather than a mutable tag and a best-effort health request.
require 'previous_image_id' "$wrapper"
require 'rollback_image_ref' "$wrapper"
require 'runtime-config' "$wrapper"
require 'runtime-env' "$wrapper"
require 'runtime-labels' "$wrapper"
require 'runtime-host-config' "$wrapper"
require 'runtime-mounts' "$wrapper"
require 'runtime-networks' "$wrapper"
require 'runtime-network-aliases' "$wrapper"
require 'helper_sha="$(field "${helper}_tool_sha256")"' "$wrapper"
require 'runtime_state create' "$wrapper"
require 'runtime_state compare' "$wrapper"
require 'persistent state restore failed' "$root/deploy/kitsusync-restore-state"
require 'STARTUP_TIMEOUT=180' "$wrapper"
require 'compare_runtime_state' "$wrapper"
require 'config-hash' "$wrapper"
require 'sha256sum -c "${backup_dir}/compose.sha256"' "$wrapper"
require 'sha256sum -c "${backup_dir}/env.sha256"' "$wrapper"

# F08: the deployment boundary never accepts or reports a Kitsu password.
reject 'KITSU_RUNTIME_PASSWORD' "$wrapper"
reject 'runtime_password' "$wrapper"
require 'kitsu.runtime_token_encrypted' "$root/docs/SETUP_WIZARD.md"

# F13/F15: source/build/image/runtime provenance needs immutable image identity
# plus matching OCI labels; a tag by itself is deliberately insufficient.
require 'build_daemon_image_id' "$wrapper"
require 'provenance' "$wrapper"
require 'artifact_kind' "$wrapper"
require 'release_commit' "$wrapper"
require 'loaded image content identity does not match approved provenance' "$wrapper"
require 'image revision label mismatch' "$wrapper"
require 'image_archive_sha256' "$wrapper"
require 'image_config_digest' "$wrapper"
require 'image_manifest_digest' "$wrapper"
require 'image_content_digest' "$wrapper"
require 'image_identity_tool_sha256' "$wrapper"
require 'archive_image_id_allows' "$wrapper"
require 'compose_sha256' "$wrapper"
require 'deployment_tool_sha256' "$wrapper"
require 'org.opencontainers.image.revision' "$dockerfile"
require 'org.opencontainers.image.source-id' "$dockerfile"
require 'org.opencontainers.image.version' "$dockerfile"
require 'COMMIT_SHA' "$compose"
require 'BUILD_SOURCE_ID' "$compose"
require 'IMAGE_REVISION' "$compose"
require 'KITSUSYNC_IMAGE_TAG' "$compose"
require 'KITSUSYNC_APP_VERSION' "$compose"
require 'VERSION' "$dockerfile"
require 'APP_ENV: production' "$wrapper"
require 'APP_ENV=development' "$compose"
require '--project-name "${PROJECT_NAME}"' "$wrapper"
[[ ! -e "$root/deploy/docker-compose.yml" ]] || {
  printf 'retired alternate production Compose file is still present\n' >&2
  exit 1
}
if grep -Eq 'loaded_image_id.*==.*(build_daemon_image_id|approved_image_id)' "$wrapper"; then
  printf 'daemon-local image identity is incorrectly treated as portable\n' >&2
  exit 1
fi

config_id=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
manifest_id=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
wrong_id=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
test_docker="$(type -P true)"
KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${test_docker}" KITSUSYNC_DEPLOY_TEST_MODE=image-id bash "$wrapper" "${config_id}" "${config_id}" "${manifest_id}"
KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${test_docker}" KITSUSYNC_DEPLOY_TEST_MODE=image-id bash "$wrapper" "${manifest_id}" "${config_id}" "${manifest_id}"
if KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${test_docker}" KITSUSYNC_DEPLOY_TEST_MODE=image-id bash "$wrapper" "${wrong_id}" "${config_id}" "${manifest_id}"; then
  printf 'unrelated daemon image ID was accepted\n' >&2
  exit 1
fi

printf 'release-hardening-contract=PASS\n'
