#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${root}/deploy/kitsusync-staging-deploy"
function_source="$(sed -n '/^verify_candidate_provenance() {/,/^}/p' "${helper}")"
[[ -n "${function_source}" ]] || { printf 'staging-provenance-contract=FAIL missing-function\n' >&2; exit 1; }
eval "${function_source}"

sha=811b52c9ec825c3757923f37ff25d23b87a8fe08
image="kitsusync:ci-${sha}"
verify_candidate_provenance "${sha}" "${sha}" "${sha}" nonrelease '' '' "${image}" "${image}"
verify_candidate_provenance "${sha}" "${sha}" "${sha}" candidate '' '' "${image}" "${image}"

! verify_candidate_provenance "${sha}" "${sha}" "${sha}" release '' '' "${image}" "${image}"
! verify_candidate_provenance "${sha}" "${sha}" "${sha}" nonrelease "${sha}" '' "${image}" "${image}"
! verify_candidate_provenance "${sha}" "${sha}" "${sha}" nonrelease '' v0.4.9 "${image}" "${image}"
! verify_candidate_provenance "${sha}" "${sha}" "$(printf '0%.0s' {1..40})" nonrelease '' '' "${image}" "${image}"
! verify_candidate_provenance "${sha}" "${sha}" "${sha}" nonrelease '' '' "kitsusync:ci-wrong" "${image}"

printf 'staging-provenance-contract=PASS candidate_and_nonrelease_exact_sha_release_rejected\n'
