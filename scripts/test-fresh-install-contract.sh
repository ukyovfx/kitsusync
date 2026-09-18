#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy="${root}/deploy/kitsusync-deploy"
bootstrap="${root}/deploy/kitsusync-bootstrap"
bundle="${root}/scripts/build-deployment-bundle.sh"
provenance="${root}/scripts/generate-provenance.sh"

KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="$(type -P true)" KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "${deploy}" fresh-install setup_required
if KITSUSYNC_DEPLOY_TEST_DOCKER_BIN="$(type -P true)" KITSUSYNC_DEPLOY_TEST_MODE=readiness bash "${deploy}" fresh-install ready; then
  printf 'fresh-install incorrectly accepted ready instead of setup_required\n' >&2
  exit 1
fi

grep -Fq 'fresh-install' "${bootstrap}"
grep -Fq 'fresh-install' "${deploy}"
grep -Fq 'fresh-install.pending' "${deploy}"
grep -Fq 'fresh install requires no prior KitsuSync container' "${deploy}"
grep -Fq 'fresh install requires an empty runtime data directory' "${deploy}"
grep -Fq 'fresh deployment validation failed; cleanup follows' "${deploy}"
grep -Fq 'compose_fresh down --remove-orphans' "${deploy}"
grep -Fq "printf 'normal\\n'" "${deploy}"
grep -Fq 'fresh install has already completed' "${bootstrap}"
grep -Fq 'fresh_conf_sha256' "${provenance}"
grep -Fq 'fresh_templates_manifest_sha256' "${provenance}"
grep -Fq 'APP_SOURCE_ROOT' "${bundle}"

if grep -Eiq '(token|password|secret|webhook|guild)[[:space:]]*=[[:space:]]*[^[:space:]#]+' "${root}/deploy/fresh-env.local"; then
  printf 'fresh environment seed contains credential-like material\n' >&2
  exit 1
fi
if grep -Eiq '(REPLACE_ME|YOUR_|example-token|[0-9]{17,20})' "${root}/deploy/fresh-conf.toml" "${root}/deploy/fresh-env.local"; then
  printf 'fresh runtime seed contains a placeholder that may be treated as configured\n' >&2
  exit 1
fi

printf 'fresh-install-contract=PASS\n'
