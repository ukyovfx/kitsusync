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
printf 'staging-candidate-download-contract=PASS\n'
