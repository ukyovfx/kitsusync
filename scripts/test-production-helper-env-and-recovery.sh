#!/usr/bin/env bash
# Production helpers pin HOME/DOCKER_CONFIG, and recovery returns to normal only
# when /ready reports ready with an authenticated, connected Kitsu runtime.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy="${root}/deploy/kitsusync-deploy"
fail() { printf 'production-helper-env-and-recovery=FAIL %s\n' "$1" >&2; exit 1; }

# Static: each root helper sets both variables after rejecting inherited DOCKER_* variables.
for helper in kitsusync-deploy kitsusync-inspect kitsusync-bootstrap; do
  file="${root}/deploy/${helper}"
  home_line="$(tr -d '\r' <"${file}" | grep -nx 'HOME=/root' | head -n1 | cut -d: -f1)"
  config_line="$(tr -d '\r' <"${file}" | grep -nx 'DOCKER_CONFIG=/root/.docker' | head -n1 | cut -d: -f1)"
  export_line="$(tr -d '\r' <"${file}" | grep -nx 'export HOME DOCKER_CONFIG' | head -n1 | cut -d: -f1)"
  reject_line="$(tr -d '\r' <"${file}" | grep -n 'inherited Docker/Compose control variables are not accepted' | head -n1 | cut -d: -f1)"
  [[ -n "${home_line}" && -n "${config_line}" && -n "${export_line}" && -n "${reject_line}" ]] || fail "${helper}: missing HOME/DOCKER_CONFIG"
  [[ "${reject_line}" -lt "${home_line}" ]] || fail "${helper}: HOME set before the inherited-variable rejection"
done

# Behavior: the recovery -> normal decision.
ready_body='{"status":"ready","build":{"base_commit":"abc"},"runtime":{"mode":"configured","kitsu":"connected","notifications":"ready","runtime_authenticated":true}}'
setup_body='{"status":"setup_required","build":{"base_commit":"abc"},"runtime":{"mode":"setup_required","kitsu":"disconnected","notifications":"paused","runtime_authenticated":false}}'
degraded_body='{"status":"degraded","build":{"base_commit":"abc"},"runtime":{"mode":"degraded","kitsu":"degraded","notifications":"degraded","runtime_authenticated":false}}'
unauth_body='{"status":"ready","build":{"base_commit":"abc"},"runtime":{"mode":"configured","kitsu":"connected","notifications":"ready","runtime_authenticated":false}}'
disconnected_body='{"status":"ready","build":{"base_commit":"abc"},"runtime":{"mode":"configured","kitsu":"disconnected","notifications":"ready","runtime_authenticated":true}}'

expect() {
  local expected="$1" label="$2" code="$3" body="$4" rc=0
  KITSUSYNC_DEPLOY_TEST_MODE=recovery-exit KITSUSYNC_DEPLOY_TEST_DOCKER_BIN=/bin/true bash "${deploy}" "${code}" "${body}" || rc=$?
  if [[ "${expected}" == pass && "${rc}" -ne 0 ]] || [[ "${expected}" == fail && "${rc}" -eq 0 ]]; then fail "${label}"; fi
}
expect pass 'ready+authenticated' 200 "${ready_body}"
expect fail 'ready body with non-200 code' 503 "${ready_body}"
expect fail 'setup_required' 503 "${setup_body}"
expect fail 'setup_required body with 200' 200 "${setup_body}"
expect fail 'degraded' 503 "${degraded_body}"
expect fail 'not authenticated' 200 "${unauth_body}"
expect fail 'kitsu disconnected' 200 "${disconnected_body}"
expect fail 'empty response' 000 ''

# The transition is wired after validation and only for recovery.
# shellcheck disable=SC2016 # literal source text, not an expansion
tr -d '\r' <"${deploy}" | grep -Fq 'if [[ "${deployment_mode}" == recovery ]]; then' || fail 'recovery transition missing'
printf 'production-helper-env-and-recovery=PASS\n'
