#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

awk '/^resolve_legacy_prior_container\(\)/ { emit=1 } emit { print } emit && /^}$/ { exit }' \
  "${ROOT}/deploy/kitsusync-bootstrap" >"${TMP}/detector.sh"

cat >"${TMP}/docker" <<'EOF'
#!/bin/bash
set -euo pipefail
scenario="${BOOTSTRAP_SCENARIO:?}"
if [[ "$1" == compose ]]; then
  if [[ "$scenario" == compose-valid ]]; then printf 'compose-id\n'; fi
  exit 0
fi
if [[ "$1" == ps ]]; then
  case "$scenario" in
    valid|wrong-name|wrong-project|wrong-service|invalid-image|wrong-ready|compose-valid) printf 'candidate-id\n' ;;
    two) printf 'candidate-a\ncandidate-b\n' ;;
  esac
  exit 0
fi
if [[ "$1" == inspect && "$2" == --format ]]; then
  format="$3"
  case "$format" in
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
[[ "${BOOTSTRAP_SCENARIO}" == wrong-ready ]] && printf '200\n' || printf '404\n'
EOF
chmod 700 "${TMP}/curl"
cat >"${TMP}/runtime-state.py" <<'EOF'
import sys
assert sys.argv[1] in ("plan", "validate")
EOF

run_case() {
  local scenario="$1" expected="$2" output
  output="$(BOOTSTRAP_SCENARIO="${scenario}" DOCKER_BIN="${TMP}/docker" CURL_BIN="${TMP}/curl" RUNTIME_STATE="${TMP}/runtime-state.py" PROJECT_NAME=kitsusync SERVICE=app EXPECTED_CONTAINER_NAME=/kitsusync-app-1 CONTROL="${TMP}" PYTHON_BIN=/usr/bin/python3 bash -c "set -e; source '${TMP}/detector.sh'; die(){ printf 'ERROR: %s\\n' \"\$*\" >&2; return 1; }; resolve_legacy_prior_container" 2>/dev/null)" || {
    [[ "${expected}" == reject ]] || { echo "unexpected rejection: ${scenario}" >&2; exit 1; }
    return
  }
  [[ "${expected}" == accept && ( "${scenario}" == compose-valid && "${output}" == compose-id || "${scenario}" != compose-valid && "${output}" == candidate-id ) ]] || { echo "unexpected acceptance: ${scenario}: ${output}" >&2; exit 1; }
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

grep -Fq 'if [[ "${mode}" == legacy-migration ]]; then' "${ROOT}/deploy/kitsusync-bootstrap"
grep -Fq 'duplicate retained service containers require operator preflight' "${ROOT}/deploy/kitsusync-deploy"
echo 'bootstrap legacy detector contract: PASS'
