#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
compose_file="${root}/deploy/kitsusync-staging-compose.yml"
source_sha="${SOURCE_SHA:?SOURCE_SHA is required}"
image_tag="${KITSUSYNC_IMAGE_TAG:?KITSUSYNC_IMAGE_TAG is required}"
image_ref="kitsusync:${image_tag}"
project=kitsusync-staging-runtime-e2e
fixture="$(mktemp -d)"
compose=(docker compose --project-name "$project" --project-directory "$fixture" --file "$compose_file")
compose_started=0

die() { printf 'staging-runtime-e2e=FAIL reason=%s\n' "$1" >&2; exit 1; }
cleanup() {
  if [[ "$compose_started" -eq 1 ]]; then
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  rm -rf -- "$fixture"
}
trap cleanup EXIT

[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || die SOURCE_SHA_INVALID
[[ "$(docker image inspect --format '{{.Config.User}}' "$image_ref")" == 10001:10001 ]] || die IMAGE_RUNTIME_USER_INVALID
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_ref")" == "$source_sha" ]] || die IMAGE_REVISION_INVALID
[[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source-id"}}' "$image_ref")" == "$source_sha" ]] || die IMAGE_SOURCE_INVALID

if docker inspect kitsusync-staging >/dev/null 2>&1; then die FIXED_CONTAINER_NAME_OCCUPIED; fi
if docker volume inspect kitsusync-staging-data >/dev/null 2>&1; then die FIXED_DATA_VOLUME_OCCUPIED; fi
if docker network inspect kitsusync-staging-network >/dev/null 2>&1; then die FIXED_NETWORK_OCCUPIED; fi

production_before="$(docker ps -q --filter publish=8090 | sort)"
install -d -m 0700 "$fixture"
cat >"$fixture/conf.toml" <<'CONFIG'
[discord]
embedsPerRequests = 10
RequestsPerMinute = 50
CONFIG
sudo chown root:root "$fixture/conf.toml"
sudo chmod 0400 "$fixture/conf.toml"

# Reproduce the installed v5 configuration contract using the actual app image
# and its configured non-root runtime identity. Keep startup output private.
set +e
docker run --rm --network none \
  --mount "type=bind,src=$fixture/conf.toml,dst=/app/conf.toml,readonly" \
  "$image_ref" >"$fixture/old-permission-startup.log" 2>&1
old_config_status=$?
set -e
[[ "$old_config_status" -ne 0 ]] || die OLD_ROOT_ONLY_CONFIG_UNEXPECTEDLY_STARTED
grep -qi 'permission denied' "$fixture/old-permission-startup.log" || die OLD_CONFIG_FAILURE_NOT_PERMISSION_DENIED
printf 'staging-runtime-e2e=reproduced-root-0400-read-failure\n'

# Migration target: root-owned config, readable only by the app's runtime GID.
sudo chown root:10001 "$fixture/conf.toml"
sudo chmod 0440 "$fixture/conf.toml"
[[ "$(stat -c '%u:%g:%a' "$fixture/conf.toml")" == 0:10001:440 ]] || die CONFIG_METADATA_INVALID
export KITSUSYNC_IMAGE_TAG="$image_tag"
"${compose[@]}" config -q || die COMPOSE_CONFIG_INVALID
compose_started=1
"${compose[@]}" up -d --force-recreate >/dev/null || die FIRST_START_FAILED

sed -n '/^wait_staging_http() {/,/^verify_staging_network_membership()/p' "$helper" | sed '$d' >"$fixture/wait-function.sh"
source "$fixture/wait-function.sh"
wait_routes() {
  local suffix="$1"
  wait_staging_http health "$source_sha" "$fixture/health-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http ready "$source_sha" "$fixture/ready-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http admin-users "$source_sha" "$fixture/users-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
  wait_staging_http admin-health "$source_sha" "$fixture/admin-health-${suffix}.json" http://127.0.0.1:8091 30 1 || return 1
}
wait_routes first || die FIRST_START_ROUTES_FAILED

container=kitsusync-staging
[[ "$(docker inspect --format '{{.Config.User}}' "$container")" == 10001:10001 ]] || die RUNNING_IDENTITY_INVALID
[[ "$(docker exec "$container" stat -c '%u:%g:%a' /app/conf.toml)" == 0:10001:440 ]] || die MOUNTED_CONFIG_METADATA_INVALID
docker exec --user 10001:10001 "$container" /bin/sh -ec 'test -r /app/conf.toml && test -w /app/data' || die RUNTIME_MOUNT_ACCESS_INVALID
[[ "$(docker port "$container" 8090/tcp)" == 127.0.0.1:8091 ]] || die LOOPBACK_BIND_INVALID
printf 'staging-runtime-e2e=first-deploy-pass health=200 ready=responded admin=responded\n'

# Exercise a second deployment of the exact image, then a failed start and
# restoration of the previously working candidate through the real Compose file.
"${compose[@]}" up -d --force-recreate >/dev/null || die SECOND_START_FAILED
wait_routes second || die SECOND_START_ROUTES_FAILED
sudo chown root:root "$fixture/conf.toml"
sudo chmod 0400 "$fixture/conf.toml"
"${compose[@]}" up -d --force-recreate >/dev/null || die FAILED_START_FIXTURE_COULD_NOT_START
if wait_staging_http health "$source_sha" "$fixture/failed-compose-health.json" http://127.0.0.1:8091 5 1 2>"$fixture/failed-compose-health.log"; then
  die ROOT_ONLY_CONFIG_COMPOSE_START_UNEXPECTEDLY_HEALTHY
fi
grep -Fq 'last_http_code=000' "$fixture/failed-compose-health.log" || die ROOT_ONLY_CONFIG_HTTP_FAILURE_NOT_OBSERVED
grep -qi 'permission denied' < <(docker logs kitsusync-staging 2>&1) || die COMPOSE_CONFIG_READ_FAILURE_NOT_OBSERVED
set +e
docker run --rm --network none --mount "type=bind,src=$fixture/conf.toml,dst=/app/conf.toml,readonly" "$image_ref" >"$fixture/rollback-failure.log" 2>&1
failed_runtime_status=$?
set -e
[[ "$failed_runtime_status" -ne 0 ]] || die FAILED_START_FIXTURE_UNEXPECTEDLY_STARTED
sudo chown root:10001 "$fixture/conf.toml"
sudo chmod 0440 "$fixture/conf.toml"
"${compose[@]}" up -d --force-recreate >/dev/null || die ROLLBACK_START_FAILED
wait_routes rollback || die ROLLBACK_ROUTES_FAILED

production_after="$(docker ps -q --filter publish=8090 | sort)"
[[ "$production_after" == "$production_before" ]] || die PRODUCTION_CONTAINER_SET_CHANGED
printf 'staging-runtime-e2e=PASS second-deploy=PASS rollback=PASS production=unchanged\n'
