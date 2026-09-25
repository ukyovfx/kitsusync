#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
candidate_deploy="${root}/scripts/deploy-kitsusync-staging-candidate.ps1"

! grep -Fq 'BundleDirectory' "${candidate_deploy}"
grep -Fq 'actions/runs?head_sha=' "${candidate_deploy}"
grep -Fq 'actions/artifacts/' "${candidate_deploy}"
grep -Fq 'actualZipDigest' "${candidate_deploy}"
grep -Fq 'Remove-Item -LiteralPath $tempRoot -Recurse -Force' "${candidate_deploy}"
grep -Fq 'image_archive_sha256' "${candidate_deploy}"
grep -Fq 'STAGING_UPLOAD_PREFLIGHT=' "${candidate_deploy}"
grep -Fq 'observed_owner_uid=' "${candidate_deploy}"
grep -Fq 'expected_owner_class=' "${candidate_deploy}"
grep -Fq 'STAGING_HELPER_CONTRACT=staging-v2' "${candidate_deploy}"
grep -Fq 'CANDIDATE_DIRECTORY_UNSAFE observed_owner_uid=' "${root}/deploy/kitsusync-staging-deploy"
grep -Fq 'STAGING_HELPER_UPGRADE=PASS' "${root}/deploy/kitsusync-staging-helper-upgrade-root.sh"
candidate_preflight_line="$(grep -n "Invoke-RemotePreflight 'vfxstudio' \$stage" "${candidate_deploy}" | head -n1 | cut -d: -f1)"
candidate_upload_line="$(grep -n 'Candidate upload failed' "${candidate_deploy}" | head -n1 | cut -d: -f1)"
candidate_deploy_preflight_line="$(grep -n "'candidate-before-deploy'" "${candidate_deploy}" | head -n1 | cut -d: -f1)"
candidate_sudo_line="$(grep -n 'sudo -n /usr/local/sbin/kitsusync-staging-deploy \$CommitSha' "${candidate_deploy}" | head -n1 | cut -d: -f1)"
[[ -n "${candidate_preflight_line}" && "${candidate_preflight_line}" -lt "${candidate_upload_line}" ]]
[[ -n "${candidate_deploy_preflight_line}" && "${candidate_deploy_preflight_line}" -lt "${candidate_sudo_line}" ]]
pwsh -NoProfile -File "${root}/scripts/test-staging-provenance-parser.ps1" -DeployScript "${candidate_deploy}"
printf 'staging-candidate-download-contract=PASS\n'
