#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wrapper="${root}/deploy/kitsusync-deploy"
image="kitsusync:${KITSUSYNC_IMAGE_TAG:?KITSUSYNC_IMAGE_TAG is required}"
suffix="$RANDOM-$$"
network="ks-state-${suffix}"
other_network="ks-state-other-${suffix}"
mount_dir="$(mktemp -d)"
other_mount_dir="$(mktemp -d)"
snapshot="$(mktemp -d)"
compat_snapshot="$(mktemp -d)"
docker_shim="$(mktemp)"
rollback_ref="kitsusync:state-rollback-${suffix}"
wrong_ref="kitsusync:state-wrong-${suffix}"
containers=()

cleanup() {
  if (("${#containers[@]}" > 0)); then
    docker rm -f "${containers[@]}" >/dev/null 2>&1 || true
  fi
  docker network rm "${network}" "${other_network}" >/dev/null 2>&1 || true
  docker image rm "${rollback_ref}" "${wrong_ref}" >/dev/null 2>&1 || true
  rm -rf "${mount_dir}" "${other_mount_dir}" "${snapshot}" "${compat_snapshot}"
  rm -f "${docker_shim}"
}
trap cleanup EXIT

expect_readiness() {
  local expected="$1" mode="$2" status="$3"
  if KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "${wrapper}" "${mode}" "${status}"; then
    [[ "${expected}" == pass ]] || { printf 'readiness unexpectedly allowed %s:%s\n' "${mode}" "${status}" >&2; exit 1; }
  else
    [[ "${expected}" == fail ]] || { printf 'readiness unexpectedly rejected %s:%s\n' "${mode}" "${status}" >&2; exit 1; }
  fi
}

expect_readiness pass normal ready
expect_readiness fail normal setup_required
expect_readiness fail normal degraded
expect_readiness pass recovery ready
expect_readiness pass recovery setup_required
expect_readiness fail recovery degraded
expect_readiness pass legacy-migration ready
expect_readiness fail legacy-migration setup_required

expect_legacy() {
  local expected="$1" approved="$2" actual="$3" health="$4" ready="$5"
  if KITSUSYNC_DEPLOY_TEST_MODE=legacy-contract bash "${wrapper}" "${approved}" "${actual}" "${health}" "${ready}"; then
    [[ "${expected}" == pass ]] || { printf 'legacy rollback contract unexpectedly passed\n' >&2; exit 1; }
  else
    [[ "${expected}" == fail ]] || { printf 'legacy rollback contract unexpectedly failed\n' >&2; exit 1; }
  fi
}
expect_legacy pass sha256:approved sha256:approved 200 404
expect_legacy fail sha256:approved sha256:wrong 200 404
expect_legacy fail sha256:approved sha256:approved 503 404
expect_legacy fail sha256:approved sha256:approved 200 200

backup_contract="$(mktemp -d)"
mkdir -p "${backup_contract}"
printf 'sqlite\n' >"${backup_contract}/sqlite.db"
if KITSUSYNC_DEPLOY_TEST_MODE=backup-contract bash "${wrapper}" "${backup_contract}"; then
  printf 'incomplete rollback backup was accepted\n' >&2; exit 1
fi
sha256sum "${backup_contract}/sqlite.db" >"${backup_contract}/persistent-state.sha256"
: >"${backup_contract}/backup-complete"
KITSUSYNC_DEPLOY_TEST_MODE=backup-contract bash "${wrapper}" "${backup_contract}"
rm -rf "${backup_contract}"

docker network create "${network}" >/dev/null
docker network create "${other_network}" >/dev/null

run_contract_container() {
  local name="$1" selected_image="$2" env_value="$3" source_mount="$4" selected_network="$5" config_hash="$6" sleep_seconds="$7" command_suffix="$8"
  docker run -d --name "${name}" \
    --network "${selected_network}" --network-alias kitsusync-app \
    --mount "type=bind,src=${source_mount},dst=/runtime-state" \
    --env "KITSUSYNC_STATE=${env_value}" \
    --env APP_ENV=production \
    --label "kitsusync.runtime-contract=test" \
    --label "com.docker.compose.config-hash=${config_hash}" \
    --entrypoint /bin/sh "${selected_image}" -c "sleep ${sleep_seconds}${command_suffix}" >/dev/null
  containers+=("${name}")
}

original="ks-original-${suffix}"
run_contract_container "${original}" "${image}" expected "${mount_dir}" "${network}" old-hash 300 ""
original_id="$(docker inspect --format '{{.Id}}' "${original}")"
image_id="$(docker inspect --format '{{.Image}}' "${original}")"
revision="$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "${original}")"
source_id="$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.source-id"}}' "${original}")"
version="$(docker inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "${original}")"
real_docker="$(command -v docker)"
cat >"${docker_shim}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == inspect && "$2" == --format && "$3" == '{{json .Config}}' ]]; then
  "${REAL_DOCKER_BIN}" "$@" | /usr/bin/python3 -c 'import json, sys; value = json.load(sys.stdin); value.pop("NetworkDisabled", None); json.dump(value, sys.stdout, separators=(",", ":"), sort_keys=True)'
elif [[ "$1" == inspect && "$2" == --format && "$3" == '{{json .HostConfig}}' ]]; then
  "${REAL_DOCKER_BIN}" "$@" | /usr/bin/python3 -c 'import json, os, sys; value = json.load(sys.stdin); value["OomKillDisable"] = True if os.environ.get("KITSUSYNC_SHIM_OOM") == "true" else None; json.dump(value, sys.stdout, separators=(",", ":"), sort_keys=True)'
else
  exec "${REAL_DOCKER_BIN}" "$@"
fi
EOF
chmod 0700 "${docker_shim}"
KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${real_docker}" KITSUSYNC_DEPLOY_TEST_MODE=snapshot bash "${wrapper}" "${original_id}" "${snapshot}"
KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${docker_shim}" REAL_DOCKER_BIN="${real_docker}" KITSUSYNC_DEPLOY_TEST_MODE=snapshot bash "${wrapper}" "${original_id}" "${compat_snapshot}"
cmp -s "${snapshot}/runtime-config" "${compat_snapshot}/runtime-config"
cmp -s "${snapshot}/runtime-host-config" "${compat_snapshot}/runtime-host-config"
grep -Fxq 'network_disabled=false' "${snapshot}/runtime-config"
/usr/bin/python3 - "${compat_snapshot}/runtime-host-config" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    assert json.load(handle)["OomKillDisable"] is False
PY
grep -Fxq 'APP_ENV=production' "${snapshot}/runtime-env"
if KITSUSYNC_SHIM_OOM=true KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="${docker_shim}" REAL_DOCKER_BIN="${real_docker}" KITSUSYNC_DEPLOY_TEST_MODE=compare \
    bash "${wrapper}" "${original_id}" "${snapshot}" "${image_id}" "${revision}" "${source_id}" "${version}"; then
  printf 'enabled OOM-kill policy mismatch was accepted\n' >&2
  exit 1
fi

docker tag "${image_id}" "${rollback_ref}"
restored="ks-restored-${suffix}"
run_contract_container "${restored}" "${rollback_ref}" expected "${mount_dir}" "${network}" new-hash 300 ""
restored_id="$(docker inspect --format '{{.Id}}' "${restored}")"
if ! KITSUSYNC_DEPLOY_TEST_MODE=compare bash "${wrapper}" "${restored_id}" "${snapshot}" "${image_id}" "${revision}" "${source_id}" "${version}"; then
  restored_snapshot="$(mktemp -d)"
  trap 'rm -rf "${restored_snapshot}"; cleanup' EXIT
  KITSUSYNC_DEPLOY_TEST_MODE=snapshot bash "${wrapper}" "${restored_id}" "${restored_snapshot}"
  for part in runtime-config runtime-env runtime-labels runtime-host-config runtime-mounts runtime-networks runtime-network-aliases; do
    cmp -s "${snapshot}/${part}" "${restored_snapshot}/${part}" || printf 'rollback normalized state mismatch: %s\n' "${part}" >&2
  done
  printf 'rollback restoration was rejected\n' >&2
  exit 1
fi

expect_mismatch() {
  local name="$1"
  local id
  id="$(docker inspect --format '{{.Id}}' "${name}")"
  if KITSUSYNC_DEPLOY_TEST_MODE=compare bash "${wrapper}" "${id}" "${snapshot}" "${image_id}" "${revision}" "${source_id}" "${version}"; then
    printf 'runtime mismatch was accepted: %s\n' "${name}" >&2
    exit 1
  fi
}

docker commit "${original}" "${wrong_ref}" >/dev/null
wrong_image="ks-wrong-image-${suffix}"
run_contract_container "${wrong_image}" "${wrong_ref}" expected "${mount_dir}" "${network}" new-hash 300 ""
expect_mismatch "${wrong_image}"

wrong_env="ks-wrong-env-${suffix}"
run_contract_container "${wrong_env}" "${rollback_ref}" changed "${mount_dir}" "${network}" new-hash 300 ""
expect_mismatch "${wrong_env}"

wrong_mount="ks-wrong-mount-${suffix}"
run_contract_container "${wrong_mount}" "${rollback_ref}" expected "${other_mount_dir}" "${network}" new-hash 300 ""
expect_mismatch "${wrong_mount}"

wrong_network="ks-wrong-network-${suffix}"
run_contract_container "${wrong_network}" "${rollback_ref}" expected "${mount_dir}" "${other_network}" new-hash 300 ""
expect_mismatch "${wrong_network}"

wrong_config="ks-wrong-config-${suffix}"
run_contract_container "${wrong_config}" "${rollback_ref}" expected "${mount_dir}" "${network}" new-hash 301 "; true"
expect_mismatch "${wrong_config}"

printf 'deployment-behavior-tests=PASS\n'
