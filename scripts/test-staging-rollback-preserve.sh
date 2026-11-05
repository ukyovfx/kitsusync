#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
function_source="$(awk '
  /^cleanup\(\) \{/ { found++; print; next }
  /^on_exit\(\) \{/ { copying = 1; found++ }
  copying { print }
  copying && /^\}/ { copying = 0 }
  END { if (found != 2) exit 1 }
' "${helper}")" || {
  printf 'staging-rollback-preserve=FAIL missing-function\n' >&2
  exit 1
}

eval "${function_source}"

work="$(mktemp -d)"
trap 'rm -rf -- "${work}"' EXIT
SOURCE_SHA=0123456789abcdef0123456789abcdef01234567
rollback_result=0
rollback_staging() { return "${rollback_result}"; }

new_case() {
  STAGE_ROOT="${work}/$1"
  PRIVATE="${STAGE_ROOT}/incoming-${SOURCE_SHA}"
  mkdir -p "${PRIVATE}"
  printf 'backup-%s\n' "$1" >"${PRIVATE}/sqlite.backup"
  printf 'secret-%s\n' "$1" >"${PRIVATE}/old.env"
}

run_on_exit() {
  # on_exit calls exit, so run it in a subshell that has already failed with status 3.
  set +e
  ( (exit 3); on_exit ) 2>"${work}/stderr"
  observed_status=$?
  set -e
  [[ "${observed_status}" -eq 3 ]] || { printf 'staging-rollback-preserve=FAIL status=%s\n' "${observed_status}" >&2; exit 1; }
}

fail() { printf 'staging-rollback-preserve=FAIL %s\n' "$1" >&2; exit 1; }
reported_path() { sed -n 's/^STAGING_ROLLBACK_BACKUP=//p' "${work}/stderr"; }

# 1. rollback fails, no pre-existing destination: moved aside with contents intact.
new_case moved
rollback_result=1
run_on_exit
dest="$(reported_path)"
[[ "${dest}" == "${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}-"* && -d "${dest}" ]] || fail "case1 dest=${dest}"
[[ ! -e "${PRIVATE}" ]] || fail 'case1 private still present'
[[ "$(cat "${dest}/sqlite.backup")" == backup-moved && "$(cat "${dest}/old.env")" == secret-moved ]] || fail 'case1 contents'

# 2. rollback fails, legacy fixed-name destination already exists: untouched, unique name used.
new_case existing
mkdir -p "${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}"
printf 'older\n' >"${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}/marker"
run_on_exit
dest="$(reported_path)"
[[ "${dest}" != "${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}" && -d "${dest}" ]] || fail "case2 dest=${dest}"
[[ "$(cat "${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}/marker")" == older ]] || fail 'case2 existing changed'
[[ "$(ls -A "${STAGE_ROOT}/failed-rollback-${SOURCE_SHA}")" == marker ]] || fail 'case2 existing polluted'
[[ "$(cat "${dest}/sqlite.backup")" == backup-existing ]] || fail 'case2 contents'

# 3. rollback fails and mv itself fails: PRIVATE stays and is the reported location.
new_case mvfail
# shellcheck disable=SC2329 # invoked indirectly by on_exit
mv() { return 1; }
run_on_exit
unset -f mv
[[ "$(reported_path)" == "${PRIVATE}" ]] || fail "case3 reported=$(reported_path)"
[[ "$(cat "${PRIVATE}/sqlite.backup")" == backup-mvfail && "$(cat "${PRIVATE}/old.env")" == secret-mvfail ]] || fail 'case3 contents'

# 4. unchanged behavior: rollback succeeds (or is unneeded) -> PRIVATE cleaned, no backup line.
new_case rolledback
rollback_result=0
run_on_exit
[[ ! -e "${PRIVATE}" ]] || fail 'case4 private not cleaned'
[[ -z "$(reported_path)" ]] || fail 'case4 unexpected backup line'

printf 'staging-rollback-preserve=PASS\n'
