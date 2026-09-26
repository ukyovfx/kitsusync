#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

awk '/^resolve_legacy_prior_container\(\)/ { emit=1 } emit { print } emit && /^}$/ { exit }' \
  "${ROOT}/deploy/kitsusync-deploy-transaction" >"${TMP}/detector.sh"

cat >"${TMP}/docker" <<'EOF'
#!/bin/bash
set -euo pipefail
scenario="${DEPLOY_SCENARIO:?}"
if [[ "$1" == ps ]]; then
  case "$scenario" in
    valid|wrong-name|wrong-project|wrong-service|invalid-image|wrong-ready|plan-fail|validate-fail) printf 'candidate-id\n' ;;
    two) printf 'candidate-a\ncandidate-b\n' ;;
  esac
  exit 0
fi
if [[ "$1" == inspect && "$2" == --format ]]; then
  case "$3" in
    '{{.Name}}') [[ "$scenario" == wrong-name ]] && printf '/other\n' || printf '/kitsusync-app-1\n' ;;
    '{{index .Config.Labels "com.docker.compose.project"}}') [[ "$scenario" == wrong-project ]] && printf 'other\n' || printf 'kitsusync\n' ;;
    '{{index .Config.Labels "com.docker.compose.service"}}') [[ "$scenario" == wrong-service ]] && printf 'web\n' || printf 'app\n' ;;
    '{{.State.Running}}') printf 'true\n' ;;
    '{{.Image}}') [[ "$scenario" == invalid-image ]] && printf 'latest\n' || printf 'sha256:%064d\n' 1 ;;
  esac
  exit 0
fi
if [[ "$1" == inspect ]]; then printf '{}\n'; exit 0; fi
exit 2
EOF
chmod 700 "${TMP}/docker"

cat >"${TMP}/curl" <<'EOF'
#!/bin/bash
[[ "${DEPLOY_SCENARIO}" == wrong-ready ]] && printf '200\n' || printf '404\n'
EOF
chmod 700 "${TMP}/curl"

cat >"${TMP}/runtime-state" <<'EOF'
#!/bin/bash
case "${DEPLOY_SCENARIO}:${1}" in
  plan-fail:plan|validate-fail:validate) exit 1 ;;
  *:plan|*:validate) exit 0 ;;
  *) exit 2 ;;
esac
EOF
chmod 700 "${TMP}/runtime-state"

run_case() {
  local scenario="$1" expected="$2" output
  output="$(DEPLOY_SCENARIO="${scenario}" DOCKER_BIN="${TMP}/docker" CURL_BIN="${TMP}/curl" RUNTIME_STATE="${TMP}/runtime-state" PROJECT_NAME=kitsusync SERVICE=app EXPECTED_CONTAINER_NAME=/kitsusync-app-1 bash -c "
    set -e
    source '${TMP}/detector.sh'
    die(){ return 1; }
    compose(){ if [[ \"\${DEPLOY_SCENARIO}\" == compose-valid ]]; then printf 'compose-id\\n'; fi; }
    runtime_state(){ \"\${RUNTIME_STATE}\" \"\$@\"; }
    resolve_legacy_prior_container
  ")" || {
    [[ "${expected}" == reject ]] || { echo "unexpected rejection: ${scenario}" >&2; exit 1; }
    return
  }
  [[ "${expected}" == accept && ( "${scenario}" == compose-valid && "${output}" == compose:compose-id || "${scenario}" != compose-valid && "${output}" == direct:candidate-id ) ]] || { echo "unexpected acceptance: ${scenario}: ${output}" >&2; exit 1; }
}

run_case valid accept
run_case compose-valid accept
run_case zero reject
run_case two reject
run_case wrong-name reject
run_case wrong-project reject
run_case wrong-service reject
run_case invalid-image reject
run_case wrong-ready reject
run_case plan-fail reject
run_case validate-fail reject

grep -Fq 'if [[ "${deployment_mode}" == legacy-migration ]]; then' "${ROOT}/deploy/kitsusync-deploy-transaction"
grep -Fq 'duplicate retained service containers require operator preflight' "${ROOT}/deploy/kitsusync-deploy-transaction"
grep -Fq 'service_ids' "${ROOT}/deploy/kitsusync-deploy-transaction"
printf 'deploy-legacy-detector-contract=PASS\n'
