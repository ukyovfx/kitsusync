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
require '"status"[[:space:]]*:[[:space:]]*"ok"' "$wrapper"

# F06: rollback is tied to an immutable image and the saved compatible runtime
# contract, rather than a mutable tag and a best-effort health request.
require 'previous_image_id' "$wrapper"
require 'rollback_image_ref' "$wrapper"
require 'runtime-config' "$wrapper"
require 'runtime-host-config' "$wrapper"
require 'runtime-mounts' "$wrapper"
require 'runtime-networks' "$wrapper"
require 'sha256sum -c "${backup_dir}/compose.sha256"' "$wrapper"
require 'sha256sum -c "${backup_dir}/env.sha256"' "$wrapper"

# F08: the deployment boundary never accepts or reports a Kitsu password.
reject 'KITSU_RUNTIME_PASSWORD' "$wrapper"
reject 'runtime_password' "$wrapper"
require 'kitsu.runtime_token_encrypted' "$root/docs/SETUP_WIZARD.md"

# F13/F15: source/build/image/runtime provenance needs immutable image identity
# plus matching OCI labels; a tag by itself is deliberately insufficient.
require 'approved-image.id' "$wrapper"
require 'provenance' "$wrapper"
require 'loaded image identity does not match approved provenance' "$wrapper"
require 'image labels do not match release provenance' "$wrapper"
require 'org.opencontainers.image.revision' "$dockerfile"
require 'org.opencontainers.image.source-id' "$dockerfile"
require 'org.opencontainers.image.version' "$dockerfile"
require 'COMMIT_SHA' "$compose"
require 'BUILD_SOURCE_ID' "$compose"
require 'IMAGE_REVISION' "$compose"

printf 'release-hardening-contract=PASS\n'
